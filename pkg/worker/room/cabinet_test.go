package room

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/giongto35/cloud-game/v3/pkg/api"
)

// slotSession keeps the last slot the worker sent to it.
type slotSession struct{ last *api.Slot }

func (s *slotSession) SendAudio([]byte, time.Duration) {}
func (s *slotSession) SendVideo([]byte, time.Duration) {}
func (s *slotSession) Disconnect()                     {}
func (s *slotSession) SendData(data []byte) {
	var m struct {
		T uint8    `json:"t"`
		P api.Slot `json:"p"`
	}
	if json.Unmarshal(data, &m) == nil && m.T == uint8(api.SlotChange) {
		s.last = &m.P
	}
}

type cabinetTest struct {
	t *testing.T
	r *GameRouter
}

func newCabinet(t *testing.T, players int) cabinetTest {
	r := NewGameRouter()
	r.SetPlayers(players)
	return cabinetTest{t, r}
}

func (c cabinetTest) join(id string) *GameSession {
	u := NewGameSession(id, &slotSession{})
	c.r.AddUser(u)
	c.r.Seat(u)
	return u
}

func (c cabinetTest) want(u *GameSession, index, queue int) {
	c.t.Helper()
	if got := slotOf(u, c.r.seated()); got != (api.Slot{Index: index, Queue: queue}) {
		c.t.Errorf("%v: got %+v, want index %d queue %d", u.Id(), got, index, queue)
	}
}

// wantSent checks the last slot message the user got.
func (c cabinetTest) wantSent(u *GameSession, index, queue int) {
	c.t.Helper()
	last := u.Session.(*slotSession).last
	if last == nil || *last != (api.Slot{Index: index, Queue: queue}) {
		c.t.Errorf("%v: was sent %+v, want index %d queue %d", u.Id(), last, index, queue)
	}
}

func TestCabinetAssignsSlotsThenWatchers(t *testing.T) {
	c := newCabinet(t, 2)
	c.r.AddUser(NewGameSession("connected, not started", &slotSession{}))

	a, b, w1, w2 := c.join("a"), c.join("b"), c.join("w1"), c.join("w2")

	c.want(a, 0, 0)
	c.want(b, 1, 0)
	c.want(w1, -1, 1)
	c.want(w2, -1, 2)
}

func TestCabinetPromotesLongestWaitingWatcher(t *testing.T) {
	c := newCabinet(t, 2)
	a, b, w1, w2 := c.join("a"), c.join("b"), c.join("w1"), c.join("w2")

	c.r.Remove(a)

	c.wantSent(w1, 0, 0) // takes the freed P1
	c.wantSent(w2, -1, 1)
	c.wantSent(b, 1, 0)

	c.r.Remove(w2) // a watcher leaving changes nobody's slot
	c.want(w1, 0, 0)
	c.want(b, 1, 0)
}

func TestCabinetLeaveAndRejoin(t *testing.T) {
	c := newCabinet(t, 2)
	a, b, w := c.join("a"), c.join("b"), c.join("w")

	c.r.Remove(b)
	c.wantSent(w, 1, 0)

	b = c.join("b") // back in a full cabinet: last in line
	c.want(b, -1, 1)

	c.r.Remove(a)
	c.wantSent(b, 0, 0)
}

func TestCabinetGivesLowestFreeSlot(t *testing.T) {
	c := newCabinet(t, 4)
	a, b, _ := c.join("a"), c.join("b"), c.join("c")

	c.r.Remove(a)
	c.r.Remove(b)

	c.want(c.join("d"), 0, 0)
	c.want(c.join("e"), 1, 0)
	c.want(c.join("f"), 3, 0)
}

func TestNotACabinet(t *testing.T) {
	c := newCabinet(t, 0)
	if c.r.Cabinet() {
		t.Fatal("players 0 must not be a cabinet")
	}
	u := NewGameSession("a", &slotSession{})
	c.r.AddUser(u)
	c.r.Remove(NewGameSession("b", &slotSession{}))
	if u.Session.(*slotSession).last != nil {
		t.Error("a slot was sent outside a cabinet")
	}
}
