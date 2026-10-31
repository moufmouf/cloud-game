package agones

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/giongto35/cloud-game/v3/pkg/logger"
)

func TestOffWithoutAgones(t *testing.T) {
	t.Setenv("AGONES_SDK_HTTP_PORT", "")
	s := New(logger.Default())
	if s != nil {
		t.Fatal("on without Agones")
	}
	s.Ready()
	s.Allocate()
}

func TestCalls(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/gameserver" {
			gets++
			if gets == 1 { // not scheduled yet
				_, _ = w.Write([]byte(`{"status":{"state":"Creating"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"object_meta":{"name":"w"},"status":{"state":"Scheduled","address":"1.2.3.4","ports":[{"name":"webrtc","port":7113}]}}`))
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	t.Setenv("AGONES_SDK_HTTP_PORT", u.Port())

	s := New(logger.Default())
	if port, err := s.Port(); port != 7113 || err != nil {
		t.Fatalf("port: %v %v", port, err)
	}
	s.Ready()
	s.Allocate()
	s.Ready()

	want := []string{"POST /ready", "POST /allocate", "POST /ready"}
	for range 50 {
		mu.Lock()
		got := slices.DeleteFunc(slices.Clone(calls), func(c string) bool { return c == "POST /health" })
		pinged := slices.Contains(calls, "POST /health")
		mu.Unlock()
		if slices.Equal(got, want) && pinged {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("calls: %v, want %v and a health ping", calls, want)
}
