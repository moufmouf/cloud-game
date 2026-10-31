package worker

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/giongto35/cloud-game/v3/pkg/logger"
	"github.com/giongto35/cloud-game/v3/pkg/worker/room"
)

func TestBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := 10 * time.Minute
		d := n

		// 10min -> 20 -> 40 -> 60 -> 60 ...
		for i := range 5 {
			backoff(&d, 1*time.Hour)

			if i > 2 {
				if d != 1*time.Hour {
					t.Errorf("cap: got %v, want 1h", d)
				}
			} else {
				x := n * 2 * time.Duration(i+1)
				if d != x {
					t.Errorf("next wait: got %v, want %v", d, x)
				}
			}
		}
	})
}

func TestDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &Worker{router: room.NewGameRouter(), log: logger.Default()}
		w.Drain(nil) // no game: at once

		drained := make(chan struct{})
		drain := func(now chan struct{}) {
			w.router.SetRoom(room.NewRoom[*room.GameSession]("game", nil, nil, nil))
			drained = make(chan struct{})
			go func() { w.Drain(now); close(drained) }()
			time.Sleep(10 * time.Second)
			synctest.Wait()
		}
		isDrained := func() bool {
			select {
			case <-drained:
				return true
			default:
				return false
			}
		}

		drain(nil)
		if isDrained() {
			t.Fatal("stopped during the game")
		}
		w.router.Close() // the game ends
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if !isDrained() {
			t.Fatal("still waiting after the game")
		}

		now := make(chan struct{}, 1)
		drain(now)
		now <- struct{}{} // a second signal
		synctest.Wait()
		if !isDrained() {
			t.Fatal("a second signal didn't stop it")
		}
		w.router.Close()
	})
}
