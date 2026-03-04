package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"cliamp-server/broadcast"
	"cliamp-server/config"
	"cliamp-server/geo"
	"cliamp-server/library"
	"cliamp-server/playlist"
	"cliamp-server/scheduler"
	"cliamp-server/server"
)

// raiseFileLimit raises the soft file-descriptor limit to the hard limit.
// Each connected listener holds one socket, so the default soft limit of
// 1024 caps the server at roughly 1000 simultaneous listeners.
func raiseFileLimit() {
	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err != nil {
		return
	}
	if rlimit.Cur < rlimit.Max {
		prev := rlimit.Cur
		rlimit.Cur = rlimit.Max
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlimit); err == nil {
			slog.Info("raised file descriptor limit", "from", prev, "to", rlimit.Max)
		}
	}
}

func main() {
	cfg, err := config.Load(config.ConfigPathFromArgs())
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if config.ParseFlags(cfg) {
		return
	}

	// Set up structured logging
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	raiseFileLimit()

	// Validate config
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build stations
	stations := make(map[string]*server.Station, len(cfg.Stations))

	for id, stCfg := range cfg.Stations {
		slog.Info("scanning library", "station", id, "path", stCfg.Path, "recursive", stCfg.Recursive)

		tracks, err := library.Scan(stCfg.Path, stCfg.Recursive)
		if err != nil {
			slog.Error("failed to scan library", "station", id, "error", err)
			os.Exit(1)
		}

		// Exclude intro file from music rotation
		if stCfg.IntroFile != "" {
			filtered := tracks[:0]
			for _, t := range tracks {
				if t.Path != stCfg.IntroFile {
					filtered = append(filtered, t)
				}
			}
			tracks = filtered
		}

		if len(tracks) == 0 {
			slog.Error("no MP3 files found", "station", id, "path", stCfg.Path)
			os.Exit(1)
		}

		pl := playlist.New(tracks, stCfg.Shuffle)

		var source playlist.TrackSource = pl

		if stCfg.AdsPath != "" {
			sched, err := scheduler.New(pl, scheduler.Config{
				AdsPath:      stCfg.AdsPath,
				AdEverySongs: stCfg.AdEveryNSongs,
				AdEveryMins:  stCfg.AdEveryNMins,
				AdShuffle:    stCfg.AdShuffle,
			})
			if err != nil {
				slog.Error("failed to create scheduler", "station", id, "error", err)
				os.Exit(1)
			}
			source = sched
			slog.Info("scheduler enabled", "station", id)
		}

		hub := broadcast.NewHub(source, cfg.Stream.BufferSize, cfg.Stream.MaxListeners)

		go hub.Run(ctx)

		stations[id] = &server.Station{
			Hub:        hub,
			Config:     stCfg,
			TrackCount: pl.Len(),
		}
	}

	// Open optional GeoIP database
	var geoDB *geo.DB
	if cfg.Geo.DBPath != "" {
		var err error
		geoDB, err = geo.Open(cfg.Geo.DBPath)
		if err != nil {
			slog.Error("failed to open geo database", "path", cfg.Geo.DBPath, "error", err)
			os.Exit(1)
		}
		defer geoDB.Close()
		slog.Info("geo database loaded", "path", cfg.Geo.DBPath)
	}

	if cfg.Admin.Password != "" {
		slog.Info("admin password protection enabled")
	} else {
		slog.Warn("admin password not set, status endpoints are public")
	}

	// Create and start HTTP server
	srv := server.New(cfg, stations, geoDB)

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
