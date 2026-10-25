package room

import (
	"slices"
	"sync"

	"github.com/giongto35/cloud-game/v3/pkg/api"
)

// Cabinet mode: a room with a fixed number of player slots, handed out by the
// worker instead of picked by the users. Each new user gets the lowest free
// slot, everyone else watches, and when a player leaves, the user who has been
// watching the longest takes over that slot.
type cabinet struct {
	mu      sync.Mutex
	players int // 0: not a cabinet, users pick their own player index
	joins   int
}

// SetPlayers makes the room a cabinet with n player slots, or not one if n <= 0.
func (r *GameRouter) SetPlayers(n int) {
	r.cab.mu.Lock()
	r.cab.players = max(n, 0)
	r.cab.mu.Unlock()
}

func (r *GameRouter) Cabinet() bool {
	r.cab.mu.Lock()
	defer r.cab.mu.Unlock()
	return r.cab.players > 0
}

// Seat gives the user the lowest free player slot, or a place in the watchers' line.
func (r *GameRouter) Seat(u *GameSession) api.Slot {
	r.cab.mu.Lock()
	defer r.cab.mu.Unlock()
	seated := r.seated()
	r.cab.joins++
	u.order = r.cab.joins
	u.Index = -1
	for i := range r.cab.players {
		if !slices.ContainsFunc(seated, func(s *GameSession) bool { return s != u && s.Index == i }) {
			u.Index = i
			break
		}
	}
	return slotOf(u, seated)
}

// Remove hands a leaving player's slot to the longest-waiting watcher and
// tells everyone left in the cabinet where they now stand.
func (r *GameRouter) Remove(u *GameSession) {
	r.cab.mu.Lock()
	inCabinet := r.cab.players > 0 && u.order > 0
	if inCabinet && u.Index >= 0 {
		var next *GameSession
		for _, s := range r.seated() {
			if s != u && s.Index < 0 && (next == nil || s.order < next.order) {
				next = s
			}
		}
		if next != nil {
			next.Index = u.Index
		}
	}
	r.cab.mu.Unlock()

	r.Router.Remove(u)

	if inCabinet {
		r.sendSlots()
	}
}

func (r *GameRouter) sendSlots() {
	r.cab.mu.Lock()
	seated := r.seated()
	msgs := make([][]byte, len(seated))
	for i, s := range seated {
		msgs[i], _ = api.Wrap(api.Out{T: uint8(api.SlotChange), Payload: slotOf(s, seated)})
	}
	r.cab.mu.Unlock()

	for i, s := range seated {
		s.SendData(msgs[i])
	}
}

// seated lists the users in the cabinet. Call it with cab.mu held.
func (r *GameRouter) seated() (ss []*GameSession) {
	for s := range r.users.Values() {
		if s.order > 0 {
			ss = append(ss, s)
		}
	}
	return ss
}

func slotOf(u *GameSession, seated []*GameSession) api.Slot {
	if u.Index >= 0 {
		return api.Slot{Index: u.Index}
	}
	q := 1
	for _, s := range seated {
		if s != u && s.Index < 0 && s.order < u.order {
			q++
		}
	}
	return api.Slot{Index: -1, Queue: q}
}
