// Package agones lets a worker run as an Agones game server (https://agones.dev). The worker tells Agones when it's
// free or running a game, so a Fleet autoscaler can keep idle workers around and never remove a busy one.
//
// It's on only when Agones runs the worker: Agones then sets AGONES_SDK_HTTP_PORT, the port of its SDK server next to
// the worker. Elsewhere New returns nil and every method does nothing.
package agones

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/giongto35/cloud-game/v3/pkg/logger"
)

type SDK struct {
	url   string
	calls chan string
	log   *logger.Logger
}

var client = http.Client{Timeout: 5 * time.Second}

func New(log *logger.Logger) *SDK {
	port := os.Getenv("AGONES_SDK_HTTP_PORT")
	if port == "" {
		return nil
	}
	s := &SDK{url: "http://localhost:" + port + "/", calls: make(chan string, 16), log: log}
	go s.run()
	return s
}

// Port returns the game server's first port. With the Passthrough port policy, the worker listens on the node port
// Agones picked for it. Agones knows it once the pod is scheduled, but its SDK server may start after the worker.
func (s *SDK) Port() (int, error) {
	for range 30 {
		var gs struct {
			Status struct {
				Ports []struct {
					Port int `json:"port"`
				} `json:"ports"`
			} `json:"status"`
		}
		if r, err := client.Get(s.url + "gameserver"); err == nil {
			err = json.NewDecoder(r.Body).Decode(&gs)
			_ = r.Body.Close()
			if err == nil && len(gs.Status.Ports) > 0 && gs.Status.Ports[0].Port > 0 {
				return gs.Status.Ports[0].Port, nil
			}
		}
		time.Sleep(time.Second)
	}
	return 0, errors.New("agones: the game server has no port")
}

// Ready tells Agones the worker is free to take a game.
func (s *SDK) Ready() { s.call("ready") }

// Allocate tells Agones the worker runs a game, so that scale-downs and updates leave it alone.
func (s *SDK) Allocate() { s.call("allocate") }

func (s *SDK) call(name string) {
	if s == nil {
		return
	}
	select {
	case s.calls <- name:
	default:
		s.log.Error().Msgf("agones: too many calls waiting, %v dropped", name)
	}
}

// run sends the calls one at a time, in order, and a health ping every 2 s (by default, Agones replaces a game server
// that misses its pings for 3 periods of 5 s).
func (s *SDK) run() {
	health := time.NewTicker(2 * time.Second)
	for {
		select {
		case name := <-s.calls:
			var err error
			for range 30 { // the SDK server may not be up yet
				if err = s.post(name); err == nil {
					break
				}
				time.Sleep(time.Second)
			}
			if err != nil {
				s.log.Error().Err(err).Msgf("agones %v", name)
			}
		case <-health.C:
			_ = s.post("health")
		}
	}
}

func (s *SDK) post(name string) error {
	r, err := client.Post(s.url+name, "application/json", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("%v", r.Status)
	}
	return nil
}
