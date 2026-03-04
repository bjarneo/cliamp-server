package broadcast

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"cliamp-server/library"
	"cliamp-server/mp3frame"
	"cliamp-server/playlist"
	"cliamp-server/transcode"
)

// Verify at compile time that *playlist.Playlist satisfies TrackSource.
var _ playlist.TrackSource = (*playlist.Playlist)(nil)

// TrackInfo holds metadata about the currently playing track.
type TrackInfo struct {
	Title  string
	Artist string
	Album  string
}

// Hub manages the broadcast: reads frames from the playlist, writes to the ring buffer,
// and manages listener connections.
type Hub struct {
	ring   *RingBuffer
	source playlist.TrackSource

	mu           sync.Mutex
	listeners    map[int64]*Listener
	nextID       int64
	maxListeners int // 0 = unlimited

	currentTrack  atomic.Value // stores TrackInfo
	listenerCount atomic.Int64

	// Stream properties (set from first frame)
	Bitrate    int
	SampleRate int
}

// NewHub creates a broadcast hub. maxListeners of 0 means unlimited.
func NewHub(source playlist.TrackSource, bufferSizeKB, maxListeners int) *Hub {
	h := &Hub{
		ring:         NewRingBuffer(bufferSizeKB * 1024),
		source:       source,
		listeners:    make(map[int64]*Listener),
		maxListeners: maxListeners,
	}
	h.currentTrack.Store(TrackInfo{})
	return h
}

// preparedTrack holds a track whose reader is already open and buffering.
type preparedTrack struct {
	track library.Track
	src   io.ReadCloser // nil if open failed
}

// Run starts the source goroutine that reads MP3 frames and writes to the ring buffer.
// It prefetches the next track's reader while the current track plays, eliminating
// the dead time between tracks where no frames reach the ring buffer.
func (h *Hub) Run(ctx context.Context) {
	prepare := func() preparedTrack {
		track := h.source.Next()
		var (
			src io.ReadCloser
			err error
		)
		if transcode.NeedsTranscode(track.Path) {
			src, err = transcode.NewReader(ctx, track.Path)
		} else {
			src, err = os.Open(track.Path)
		}
		if err != nil {
			slog.Error("cannot open track", "path", track.Path, "error", err)
		}
		return preparedTrack{track: track, src: src}
	}

	next := prepare()

	for {
		if ctx.Err() != nil {
			if next.src != nil {
				next.src.Close()
			}
			return
		}

		current := next

		// Skip tracks that failed to open.
		if current.src == nil {
			next = prepare()
			continue
		}

		// Start preparing the next track while the current one plays.
		// By the time the current track ends, ffmpeg is already running
		// with data buffered in the OS pipe (~64KB ≈ 4s of MP3 at 128k).
		nextCh := make(chan preparedTrack, 1)
		go func() { nextCh <- prepare() }()

		h.streamTrack(ctx, current)

		// Close the finished source in the background so the next track's
		// first frame reaches the ring buffer without waiting for ffmpeg
		// process cleanup.  This eliminates the between-track gap that
		// permanently depletes the client's playback buffer.
		go current.src.Close()

		next = <-nextCh
	}
}

// streamTrack plays a single prepared track into the ring buffer at real-time speed.
func (h *Hub) streamTrack(ctx context.Context, pt preparedTrack) {
	slog.Info("now playing", "title", pt.track.Title, "artist", pt.track.Artist, "path", pt.track.Path)

	h.currentTrack.Store(TrackInfo{
		Title:  pt.track.Title,
		Artist: pt.track.Artist,
		Album:  pt.track.Album,
	})

	reader, err := mp3frame.NewReader(pt.src)
	if err != nil {
		slog.Error("cannot create frame reader", "path", pt.track.Path, "error", err)
		return
	}

	var (
		epoch        = time.Now()
		totalSamples int64
		sampleRate   int
	)

	// Reusable timer avoids per-frame allocation leak from time.After.
	throttle := time.NewTimer(0)
	if !throttle.Stop() {
		<-throttle.C
	}
	defer throttle.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		frame, err := reader.ReadFrame()
		if err != nil {
			slog.Debug("end of track", "path", pt.track.Path, "error", err)
			return
		}

		// Set stream properties from first frame
		if sampleRate == 0 {
			sampleRate = frame.SampleRate
			h.Bitrate = frame.Bitrate
			h.SampleRate = frame.SampleRate
		}

		// Write frame to ring buffer
		h.ring.Write(frame.Data)

		// Throttle to real-time
		totalSamples += int64(frame.Samples)
		if sampleRate > 0 {
			deadline := epoch.Add(time.Duration(float64(totalSamples) / float64(sampleRate) * float64(time.Second)))
			if wait := time.Until(deadline); wait > 0 {
				throttle.Reset(wait)
				select {
				case <-throttle.C:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// AddListener registers a new listener and returns it.
// Returns ErrFull if the maximum listener count has been reached.
func (h *Hub) AddListener(wantMeta bool, info ListenerInfo) (*Listener, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.maxListeners > 0 && len(h.listeners) >= h.maxListeners {
		return nil, ErrFull
	}

	id := h.nextID
	h.nextID++

	pos := h.ring.PrerollPos()
	l := NewListener(id, pos, wantMeta, info)
	h.listeners[id] = l
	h.listenerCount.Add(1)

	slog.Info("listener connected", "id", id, "ip", info.IP, "total", h.listenerCount.Load())
	return l, nil
}

// ListenerSnapshot holds a point-in-time view of a listener for status reporting.
type ListenerSnapshot struct {
	ID          int64
	Info        ListenerInfo
	ConnectedAt time.Time
}

// Listeners returns a snapshot of all currently connected listeners.
func (h *Hub) Listeners() []ListenerSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snaps := make([]ListenerSnapshot, 0, len(h.listeners))
	for _, l := range h.listeners {
		snaps = append(snaps, ListenerSnapshot{
			ID:          l.ID,
			Info:        l.Info,
			ConnectedAt: l.ConnectedAt,
		})
	}
	return snaps
}

// RemoveListener unregisters a listener.
func (h *Hub) RemoveListener(l *Listener) {
	l.Close()

	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.listeners, l.ID)
	h.listenerCount.Add(-1)

	slog.Info("listener disconnected", "id", l.ID, "total", h.listenerCount.Load())
}

// ListenerCount returns the current number of connected listeners.
func (h *Hub) ListenerCount() int {
	return int(h.listenerCount.Load())
}

// CurrentTrack returns info about the currently playing track.
func (h *Hub) CurrentTrack() TrackInfo {
	return h.currentTrack.Load().(TrackInfo)
}

// Ring returns the hub's ring buffer for listener reads.
func (h *Hub) Ring() *RingBuffer {
	return h.ring
}
