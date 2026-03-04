package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"cliamp-server/broadcast"
	"cliamp-server/config"
	"cliamp-server/geo"
	"cliamp-server/handler"
)

// Station holds the per-station runtime state.
type Station struct {
	Hub        *broadcast.Hub
	Config     config.StationConfig
	TrackCount int
}

// Server wraps the HTTP server and route configuration.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	startTime  time.Time
}

// New creates a new server with all routes configured for multiple stations.
// geoDB may be nil if no MaxMind database is configured.
func New(cfg *config.Config, stations map[string]*Station, geoDB *geo.DB) *Server {
	s := &Server{
		cfg:       cfg,
		startTime: time.Now(),
	}

	mux := http.NewServeMux()

	// Per-station routes
	for id, st := range stations {
		prefix := "/" + id

		mux.Handle(prefix+"/stream", &handler.Stream{
			Hub:       st.Hub,
			MetaInt:   cfg.Stream.MetaInt,
			Name:      st.Config.Name,
			Genre:     st.Config.Genre,
			URL:       st.Config.URL,
			IntroFile: st.Config.IntroFile,
			GeoDB:     geoDB,
		})

		mux.Handle(prefix+"/stream.pls", &handler.PlaylistPLS{
			Name:   st.Config.Name,
			Prefix: id,
		})

		mux.Handle(prefix+"/stream.m3u", &handler.PlaylistM3U{
			Name:   st.Config.Name,
			Prefix: id,
		})

		mux.Handle(prefix+"/status", &handler.Status{
			Hub:         st.Hub,
			StartTime:   s.startTime,
			Password:    cfg.Admin.Password,
			TrackCount:  st.TrackCount,
			StationName: st.Config.Name,
		})

		slog.Info("station registered",
			"id", id,
			"name", st.Config.Name,
			"tracks", st.TrackCount,
			"prefix", prefix,
		)
	}

	// Global status route
	stationInfos := make(map[string]*handler.StationInfo, len(stations))
	for id, st := range stations {
		stationInfos[id] = &handler.StationInfo{
			Hub:        st.Hub,
			Name:       st.Config.Name,
			TrackCount: st.TrackCount,
		}
	}

	mux.Handle("/status", &handler.GlobalStatus{
		Stations:  stationInfos,
		StartTime: s.startTime,
		Password:  cfg.Admin.Password,
	})

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return s
}

// ListenAndServe starts the HTTP server with TCP_NODELAY enabled
// so each Flush sends a TCP packet immediately instead of being
// batched by Nagle's algorithm.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	slog.Info("server starting", "addr", s.httpServer.Addr)
	return s.httpServer.Serve(&noDelayListener{ln})
}

// noDelayListener wraps a net.Listener and sets TCP_NODELAY on every
// accepted connection.  For a ~128 kbps audio stream the individual
// writes are small (~400 B); without TCP_NODELAY, Nagle's algorithm
// can delay them by up to 200 ms while waiting for ACKs.
type noDelayListener struct {
	net.Listener
}

func (l *noDelayListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	return c, nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}
