package scheduler

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cliamp-server/library"
)

// audioExtensions recognized by the ad pool (same set as library scanner).
var audioExtensions = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
	".opus": true,
	".m4a":  true,
	".aac":  true,
	".webm": true,
	".wma":  true,
}

// AdPool manages a pool of ad tracks to play between songs.
type AdPool struct {
	mu      sync.Mutex
	tracks  []library.Track
	pos     int
	shuffle bool
}

// NewAdPool creates an ad pool from a path (file or directory).
// If path is a single file, the pool contains that one track.
// If path is a directory, all .mp3 files in it (non-recursive) are loaded.
// Returns an error if the directory is empty or the path is invalid.
func NewAdPool(path string, shuffle bool) (*AdPool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("ads path %q: %w", path, err)
	}

	var tracks []library.Track

	if !info.IsDir() {
		// Single file
		tracks = append(tracks, library.Track{
			Path:  path,
			Title: "Ad",
		})
	} else {
		// Directory: scan for audio files (flat, non-recursive)
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("reading ads directory %q: %w", path, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if audioExtensions[ext] {
				tracks = append(tracks, library.Track{
					Path:  filepath.Join(path, e.Name()),
					Title: "Ad",
				})
			}
		}
		if len(tracks) == 0 {
			return nil, fmt.Errorf("no audio files found in ads directory %q", path)
		}
	}

	pool := &AdPool{
		tracks:  tracks,
		pos:     -1,
		shuffle: shuffle,
	}

	if shuffle {
		pool.shuffleTracks()
	}

	return pool, nil
}

// Next returns the next ad track.
func (p *AdPool) Next() library.Track {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pos++
	if p.pos >= len(p.tracks) {
		if p.shuffle {
			p.shuffleTracks()
		}
		p.pos = 0
	}

	return p.tracks[p.pos]
}

func (p *AdPool) shuffleTracks() {
	rand.Shuffle(len(p.tracks), func(i, j int) {
		p.tracks[i], p.tracks[j] = p.tracks[j], p.tracks[i]
	})
}
