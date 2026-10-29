package coordinator

import (
	"net/url"
	"slices"
	"sync"
	"testing"

	"github.com/giongto35/cloud-game/v3/pkg/api"
	"github.com/giongto35/cloud-game/v3/pkg/com"
	"github.com/giongto35/cloud-game/v3/pkg/config"
	"github.com/giongto35/cloud-game/v3/pkg/logger"
)

// fakeConn records the packet types sent through it.
type fakeConn struct {
	id   com.Uid
	mu   sync.Mutex
	sent []api.PT
}

func (f *fakeConn) Disconnect()                                              {}
func (f *fakeConn) Id() com.Uid                                              { return f.id }
func (f *fakeConn) ProcessPackets(func(api.In[com.Uid]) error) chan struct{} { return nil }
func (f *fakeConn) Send(t api.PT, _ any) ([]byte, error)                     { f.Notify(t, nil); return nil, nil }
func (f *fakeConn) Notify(t api.PT, _ any) {
	f.mu.Lock()
	f.sent = append(f.sent, t)
	f.mu.Unlock()
}
func (f *fakeConn) got(t api.PT) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.sent, t)
}

func newTestHub(workers int) (*Hub, []*Worker) {
	h := NewHub(config.CoordinatorConfig{}, logger.Default())
	var ww []*Worker
	for range workers {
		w := &Worker{Connection: &fakeConn{id: com.NewUid()}, log: logger.Default()}
		h.workers.Add(w)
		ww = append(ww, w)
	}
	return h, ww
}

func newTestUser(w *Worker) (*User, *fakeConn) {
	c := &fakeConn{id: com.NewUid()}
	return &User{Connection: c, w: w, log: logger.Default()}, c
}

func roomQuery(id string) url.Values { return url.Values{api.RoomIdQueryParam: {id}} }

func TestFindWorkerForArrivalsTogether(t *testing.T) {
	h, _ := newTestHub(4)

	got := make([]*Worker, 20)
	var wg sync.WaitGroup
	for i := range got {
		wg.Go(func() { got[i] = h.findWorkerFor(nil, roomQuery("cab___game"), logger.Default()) })
	}
	wg.Wait()

	if got[0] == nil {
		t.Fatal("no worker")
	}
	for i, w := range got {
		if w != got[0] {
			t.Fatalf("arrival %d got another worker: two games for one room", i)
		}
	}

	other := h.findWorkerFor(nil, roomQuery("other___game"), logger.Default())
	if other == nil || other == got[0] {
		t.Errorf("another room must get another free worker, got %v", other)
	}
}

func TestStartGameJoinsRoomStillLoading(t *testing.T) {
	h, ww := newTestHub(1)
	w := ww[0]
	if h.findWorkerFor(nil, roomQuery("cab___game"), logger.Default()) != w {
		t.Fatal("no worker")
	}
	w.TryReserve() // the first user's StartGame is in flight, the room isn't registered yet

	bob, bobConn := newTestUser(w)
	bob.HandleStartGame(api.GameStartUserRequest{RoomId: "cab___game"}, config.CoordinatorConfig{})
	if bobConn.got(api.ErrNoFreeSlots) {
		t.Error("a user for the loading room got no free slots")
	}
	if !w.Connection.(*fakeConn).got(api.StartGame) {
		t.Error("the join was not forwarded to the worker")
	}

	carol, carolConn := newTestUser(w)
	carol.HandleStartGame(api.GameStartUserRequest{RoomId: "other___game"}, config.CoordinatorConfig{})
	if !carolConn.got(api.ErrNoFreeSlots) {
		t.Error("a user for another room got onto a busy worker")
	}
}

func TestCloseRoomFreesUnregisteredRoom(t *testing.T) {
	h, ww := newTestHub(1)
	w := ww[0]
	h.findWorkerFor(nil, roomQuery("cab___game"), logger.Default())
	w.TryReserve()

	w.HandleCloseRoom("cab___game") // the game failed to load

	if !w.HasSlot() || w.Hosts("cab___game") {
		t.Error("the worker is still held by a room that never started")
	}
}

func TestStartGameRefusesUnknownGame(t *testing.T) {
	h, ww := newTestHub(1)
	w := ww[0]
	w.SetLib([]api.GameInfo{{Name: "Super Game (U)"}})
	room := "cab___Super Game (U).sfc" // a file name instead of the game name
	h.findWorkerFor(nil, roomQuery(room), logger.Default())

	u, conn := newTestUser(w)
	u.HandleStartGame(api.GameStartUserRequest{RoomId: room, GameName: "Super Game (U).sfc"}, config.CoordinatorConfig{})

	if !conn.got(api.ErrUnknownGame) {
		t.Error("the user wasn't told the game is unknown")
	}
	if w.Connection.(*fakeConn).got(api.StartGame) {
		t.Error("the unknown game was sent to the worker")
	}
	if !w.HasSlot() || w.Hosts(room) {
		t.Fatal("the worker is still held for a game it doesn't have")
	}

	other := "other___Super Game (U)"
	if h.findWorkerFor(nil, roomQuery(other), logger.Default()) != w {
		t.Fatal("the worker isn't free for another cabinet")
	}
	u2, conn2 := newTestUser(w)
	u2.HandleStartGame(api.GameStartUserRequest{RoomId: other}, config.CoordinatorConfig{})
	if conn2.got(api.ErrUnknownGame) || !w.Connection.(*fakeConn).got(api.StartGame) {
		t.Error("a known game didn't start")
	}
}

func TestSuggestGame(t *testing.T) {
	lib := []api.GameInfo{{Name: "Supercooked! (J) (v1.2)"}, {Name: "anguna"}}
	for name, want := range map[string]string{
		"Supercooked! (J) (v1.2).sfc": "Supercooked! (J) (v1.2)",
		"Anguna":                      "anguna",
		"anguna.gba":                  "anguna",
		"Tetris":                      "",
		"":                            "",
	} {
		if got := suggestGame(name, lib); got != want {
			t.Errorf("suggestGame(%q) = %q, want %q", name, got, want)
		}
	}
}
