package coordinator

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/giongto35/cloud-game/v3/pkg/config"
	"github.com/giongto35/cloud-game/v3/pkg/logger"
	"github.com/giongto35/cloud-game/v3/pkg/monitoring"
	"github.com/giongto35/cloud-game/v3/pkg/network/httpx"
)

type Coordinator struct {
	hub      *Hub
	services [2]interface {
		Run()
		Stop() error
	}
}

func New(conf config.CoordinatorConfig, log *logger.Logger) (*Coordinator, error) {
	coordinator := &Coordinator{hub: NewHub(conf, log)}
	h, err := NewHTTPServer(conf, log, func(mux *httpx.Mux) *httpx.Mux {
		mux.HandleFunc("/ws", coordinator.hub.handleUserConnection())
		mux.HandleFunc("/wso", coordinator.hub.handleWorkerConnection())
		return mux
	})
	if err != nil {
		return nil, fmt.Errorf("http init fail: %w", err)
	}
	coordinator.services[0] = h
	if conf.Coordinator.Monitoring.IsEnabled() {
		coordinator.services[1] = monitoring.New(conf.Coordinator.Monitoring, h.GetHost(), log)
	}
	return coordinator, nil
}

func (c *Coordinator) Start() {
	for _, s := range c.services {
		if s != nil {
			s.Run()
		}
	}
}

func (c *Coordinator) Stop() error {
	var err error
	for _, s := range c.services {
		if s != nil {
			err0 := s.Stop()
			err = errors.Join(err, err0)
		}
	}
	return err
}

func NewHTTPServer(conf config.CoordinatorConfig, log *logger.Logger, fnMux func(*httpx.Mux) *httpx.Mux) (*httpx.Server, error) {
	return httpx.NewServer(
		conf.Coordinator.Server.GetAddr(),
		func(s *httpx.Server) httpx.Handler { return fnMux(s.Mux().Handle("/", index(conf, log))) },
		httpx.WithServerConfig(conf.Coordinator.Server),
		httpx.WithLogger(log),
	)
}

func index(conf config.CoordinatorConfig, log *logger.Logger) httpx.Handler {
	parse := func() *template.Template {
		return template.Must(template.ParseFiles("./web/index.html", "./web/cabinet.html"))
	}
	pagesTpl := parse()

	// render the pages with some tpl values
	tplData := struct {
		Analytics   config.Analytics
		Recording   config.Recording
		WaIframeApi string
	}{conf.Coordinator.Analytics, conf.Recording, conf.Coordinator.WaIframeApi}

	// page is the template name of the path, or "" for a static file
	page := func(path string) string {
		if path == "/" || strings.HasSuffix(path, "/index.html") {
			return "index.html"
		}
		if strings.HasSuffix(path, "/cabinet.html") {
			return "cabinet.html"
		}
		return ""
	}

	handler := func(tpl *template.Template, name string, w httpx.ResponseWriter, r *httpx.Request) {
		if err := tpl.ExecuteTemplate(w, name, tplData); err != nil {
			log.Error().Err(err).Msgf("error with the %v template file", name)
		}
	}

	h := httpx.FileServer("./web")

	if conf.Coordinator.Debug {
		log.Info().Msgf("Using auto-reloading index.html")
		return httpx.HandlerFunc(func(w httpx.ResponseWriter, r *httpx.Request) {
			if conf.Coordinator.Server.CacheControl != "" {
				w.Header().Add("Cache-Control", conf.Coordinator.Server.CacheControl)
			}
			if conf.Coordinator.Server.FrameOptions != "" {
				w.Header().Add("X-Frame-Options", conf.Coordinator.Server.FrameOptions)
			}
			if name := page(r.URL.Path); name != "" {
				handler(parse(), name, w, r)
				return
			}
			h.ServeHTTP(w, r)
		})
	}

	return httpx.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conf.Coordinator.Server.CacheControl != "" {
			w.Header().Add("Cache-Control", conf.Coordinator.Server.CacheControl)
		}
		if conf.Coordinator.Server.FrameOptions != "" {
			w.Header().Add("X-Frame-Options", conf.Coordinator.Server.FrameOptions)
		}
		if name := page(r.URL.Path); name != "" {
			handler(pagesTpl, name, w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
