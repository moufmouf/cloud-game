package coordinator

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/giongto35/cloud-game/v3/pkg/api"
	"github.com/giongto35/cloud-game/v3/pkg/config"
	"github.com/giongto35/cloud-game/v3/pkg/games"
)

func (u *User) HandleInitWebrtcStream(rq api.InitUserWebrtcStreamRequest) {
	if u.w == nil {
		u.log.Warn().Msg("no worker assigned")
		return
	}
	resp, err := u.w.InitWebrtcStream(u.Id().String(), rq.Initiator, rq.Sdp)
	if err != nil || resp == nil || *resp == api.EMPTY {
		u.log.Error().Err(err).Msg("malformed WebRTC init response")
		return
	}
	u.SendWebrtcOffer(string(*resp))
}

func (u *User) HandleWebrtcSignal(rq api.WebrtcSignalUser) {
	u.w.WebrtcSignal(u.Id().String(), rq.Sdp, rq.Ice)
}

func (u *User) HandleStartGame(rq api.GameStartUserRequest, conf config.CoordinatorConfig) {
	// Refuse a game the worker doesn't have before reserving the worker: it would
	// fail to start the game, and nothing would free the reservation.
	if lib := u.w.AppNames(); lib != nil { // nil until the worker sends its library
		name := rq.GameName
		if rq.RoomId != "" {
			name = games.ExtractGame(rq.RoomId) // as the worker does
		}
		if !slices.ContainsFunc(lib, func(g api.GameInfo) bool { return g.Name == name }) {
			u.log.Warn().Str("game", name).Msg("unknown game")
			u.w.pending.Drop(rq.RoomId)
			u.Notify(api.ErrUnknownGame, suggestGame(name, lib))
			return
		}
	}

	// Worker slot / room gating:
	// - If the worker is FREE, reserve its single slot and start the game.
	// - If it is BUSY, only let through requests for the room it runs, or is
	//   about to run (claimed in findWorkerFor but still loading). The worker
	//   handles StartGame calls one at a time, so a join that arrives during
	//   the load is answered once the room exists.
	//   When the user is starting a NEW game (empty room id), we give the
	//   worker a short grace period to close the previous room and free the
	//   slot before rejecting with "no slots".

	// Grace period: when there's no room id in the request (new game) but the
	// worker still appears busy, wait a bit for the previous room to close.
	if rq.RoomId == "" && !u.w.HasSlot() {
		const waitTotal = 3 * time.Second
		const step = 100 * time.Millisecond
		waited := time.Duration(0)
		for waited < waitTotal {
			if u.w.HasSlot() {
				break
			}
			time.Sleep(step)
			waited += step
		}
	}

	if !u.w.TryReserve() && !u.w.Hosts(rq.RoomId) {
		u.Notify(api.ErrNoFreeSlots, "")
		return
	}

	startGameResp, err := u.w.StartGame(u.Id().String(), rq)
	if err != nil || startGameResp == nil {
		u.log.Error().Err(err).Msg("malformed game start response")
		return
	}
	if startGameResp.Rid == "" {
		u.log.Error().Msg("there is no room")
		return
	}
	u.log.Info().Str("id", startGameResp.Rid).Msg("Received room response from worker")
	u.StartGame(startGameResp.AV, startGameResp.KbMouse, startGameResp.Slot)

	// send back recording status
	if conf.Recording.Enabled && rq.Record {
		u.Notify(api.RecordGame, api.OK)
	}
}

func (u *User) HandleQuitGame(rq api.GameQuitRequest) {
	if rq.Rid == u.w.RoomId {
		u.w.QuitGame(u.Id().String())
	}
}

func (u *User) HandleResetGame(rq api.ResetGameRequest) {
	if rq.Rid != u.w.RoomId {
		return
	}
	u.w.ResetGame(u.Id().String())
}

func (u *User) HandleSaveGame() error {
	resp, err := u.w.SaveGame(u.Id().String())
	if err != nil {
		return err
	}

	if *resp == api.OK {
		if id, _ := api.ExplodeDeepLink(u.w.RoomId); id != "" {
			u.w.AddSession(id)
		}
	}

	u.Notify(api.SaveGame, resp)
	return nil
}

func (u *User) HandleLoadGame() error {
	resp, err := u.w.LoadGame(u.Id().String())
	if err != nil {
		return err
	}
	u.Notify(api.LoadGame, resp)
	return nil
}

func (u *User) HandleChangePlayer(rq api.ChangePlayerUserRequest) {
	resp, err := u.w.ChangePlayer(u.Id().String(), int(rq))
	// !to make it a little less convoluted
	if err != nil || resp == nil || *resp == -1 {
		u.log.Error().Err(err).Msgf("player select fail, req: %v", rq)
		return
	}
	u.Notify(api.ChangePlayer, rq)
}

func (u *User) HandleRecordGame(rq api.RecordGameRequest) {
	if u.w == nil {
		return
	}

	u.log.Debug().Msgf("??? room: %v, rec: %v user: %v", u.w.RoomId, rq.Active, rq.User)

	if u.w.RoomId == "" {
		u.log.Error().Msg("Recording in the empty room is not allowed!")
		return
	}

	resp, err := u.w.RecordGame(u.Id().String(), rq.Active, rq.User)
	if err != nil {
		u.log.Error().Err(err).Msg("malformed game record request")
		return
	}
	u.Notify(api.RecordGame, resp)
}

func (u *User) handleGetWorkerList(debug bool, info HasServerInfo) {
	response := api.GetWorkerListResponse{}
	servers := info.GetServerList()

	if debug {
		response.Servers = servers
	} else {
		unique := map[string]*api.Server{}
		for _, s := range servers {
			mid := s.Machine
			if _, ok := unique[mid]; !ok {
				unique[mid] = &api.Server{Addr: s.Addr, PingURL: s.PingURL, Id: s.Id, InGroup: true}
			}
			v := unique[mid]
			if v != nil {
				v.Replicas++
			}
		}
		for _, v := range unique {
			response.Servers = append(response.Servers, *v)
		}
	}
	if len(response.Servers) > 0 {
		sort.SliceStable(response.Servers, func(i, j int) bool {
			if response.Servers[i].Addr != response.Servers[j].Addr {
				return response.Servers[i].Addr < response.Servers[j].Addr
			}
			return response.Servers[i].Port < response.Servers[j].Port
		})
	}
	u.Notify(api.GetWorkerList, response)
}

// suggestGame returns the game a wrong name most likely meant: the same name in
// another case, or without its file extension. Empty when there's none.
func suggestGame(name string, lib []api.GameInfo) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, g := range lib {
		if strings.EqualFold(g.Name, name) || strings.EqualFold(g.Name, base) {
			return g.Name
		}
	}
	return ""
}
