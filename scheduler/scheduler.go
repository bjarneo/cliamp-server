package scheduler

import (
	"time"

	"cliamp-server/library"
	"cliamp-server/playlist"
)

// Config holds the configuration for a Scheduler.
type Config struct {
	AdsPath      string // Path to ads directory or single file (empty = no ads)
	AdEverySongs int    // Play ad after every N music tracks (0 = disabled)
	AdEveryMins  int    // Play ad after every N minutes (0 = disabled)
	AdShuffle    bool   // Randomize ad selection
}

// Scheduler sits between the Hub and Playlist, injecting ad tracks.
// It implements playlist.TrackSource.
type Scheduler struct {
	music        playlist.TrackSource
	adPool       *AdPool
	adEverySongs int
	adEveryMins  int
	songsSinceAd int
	lastAdTime   time.Time
}

// New creates a Scheduler wrapping the given music source.
// Returns an error if ad pool creation fails.
func New(music playlist.TrackSource, cfg Config) (*Scheduler, error) {
	s := &Scheduler{
		music:        music,
		adEverySongs: cfg.AdEverySongs,
		adEveryMins:  cfg.AdEveryMins,
		lastAdTime:   time.Now(),
	}

	if cfg.AdsPath != "" {
		pool, err := NewAdPool(cfg.AdsPath, cfg.AdShuffle)
		if err != nil {
			return nil, err
		}
		s.adPool = pool
	}

	return s, nil
}

// Next returns the next track to play: ads (periodic) or music.
func (s *Scheduler) Next() library.Track {
	// 1. Ads: check if a trigger has fired
	if s.adPool != nil && s.shouldPlayAd() {
		s.songsSinceAd = 0
		s.lastAdTime = time.Now()
		return s.adPool.Next()
	}

	// 2. Regular music
	s.songsSinceAd++
	return s.music.Next()
}

// shouldPlayAd returns true if either the song count or time trigger has fired.
func (s *Scheduler) shouldPlayAd() bool {
	if s.adEverySongs > 0 && s.songsSinceAd >= s.adEverySongs {
		return true
	}
	if s.adEveryMins > 0 && time.Since(s.lastAdTime) >= time.Duration(s.adEveryMins)*time.Minute {
		return true
	}
	return false
}
