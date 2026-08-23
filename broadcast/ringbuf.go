package broadcast

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
)

const prerollFrames = 128 // About 3.3 seconds at the fixed 128 kbps output profile.

type ringTrackEvent struct {
	pos   int64
	track TrackInfo
}

// RingBuffer is a single-writer, multi-reader circular buffer with absolute positioning.
// Readers use a shared lock so they can copy concurrently. Track changes are stored at
// absolute byte positions so delayed listeners receive metadata matching their audio.
type RingBuffer struct {
	rw       sync.RWMutex
	data     []byte
	size     int
	writePos atomic.Int64

	notifMu sync.Mutex
	notif   chan struct{}
	closed  chan struct{}
	close   sync.Once

	frameHistory [prerollFrames]int64
	frameIdx     int
	frameCount   int
	trackEvents  []ringTrackEvent
}

// NewRingBuffer creates a ring buffer of the given size in bytes.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data:   make([]byte, size),
		size:   size,
		notif:  make(chan struct{}),
		closed: make(chan struct{}),
	}
}

// Write appends one complete MP3 frame to the ring buffer.
func (rb *RingBuffer) Write(p []byte) {
	rb.WriteFrame(p, nil)
}

// WriteFrame appends one complete MP3 frame and optionally records a track
// transition at the frame's first byte.
func (rb *RingBuffer) WriteFrame(p []byte, transition *TrackInfo) {
	rb.rw.Lock()

	pos := rb.writePos.Load()
	rb.frameHistory[rb.frameIdx] = pos
	rb.frameIdx = (rb.frameIdx + 1) % prerollFrames
	if rb.frameCount < prerollFrames {
		rb.frameCount++
	}
	if transition != nil {
		rb.trackEvents = append(rb.trackEvents, ringTrackEvent{pos: pos, track: *transition})
	}

	start := int(pos) % rb.size
	if start+len(p) <= rb.size {
		copy(rb.data[start:], p)
	} else {
		first := rb.size - start
		copy(rb.data[start:], p[:first])
		copy(rb.data, p[first:])
	}

	newWritePos := rb.writePos.Add(int64(len(p)))
	rb.pruneTrackEvents(newWritePos - int64(rb.size))
	rb.rw.Unlock()

	rb.notifMu.Lock()
	close(rb.notif)
	rb.notif = make(chan struct{})
	rb.notifMu.Unlock()
}

func (rb *RingBuffer) pruneTrackEvents(cutoff int64) {
	keep := 0
	for keep+1 < len(rb.trackEvents) && rb.trackEvents[keep+1].pos <= cutoff {
		keep++
	}
	if keep == 0 {
		return
	}
	copy(rb.trackEvents, rb.trackEvents[keep:])
	clear(rb.trackEvents[len(rb.trackEvents)-keep:])
	rb.trackEvents = rb.trackEvents[:len(rb.trackEvents)-keep]
}

// WritePos returns the current absolute write position.
func (rb *RingBuffer) WritePos() int64 {
	return rb.writePos.Load()
}

// PrerollPos returns the oldest retained frame among the recent frame history.
func (rb *RingBuffer) PrerollPos() int64 {
	rb.rw.RLock()
	defer rb.rw.RUnlock()

	if rb.frameCount == 0 {
		return 0
	}

	writePos := rb.writePos.Load()
	oldest := (rb.frameIdx - rb.frameCount + prerollFrames) % prerollFrames
	for i := 0; i < rb.frameCount; i++ {
		pos := rb.frameHistory[(oldest+i)%prerollFrames]
		if writePos-pos <= int64(rb.size) {
			return pos
		}
	}
	return writePos
}

// Read reads up to len(p) bytes from pos and blocks until data is available.
// A read never crosses a track transition. The returned TrackInfo describes all
// returned bytes. ErrSlow is returned if pos has been overwritten.
func (rb *RingBuffer) Read(ctx context.Context, pos int64, p []byte) (int, int64, TrackInfo, error) {
	for {
		writePos := rb.writePos.Load()
		if pos < writePos {
			rb.rw.RLock()

			writePos = rb.writePos.Load()
			if writePos-pos > int64(rb.size) {
				rb.rw.RUnlock()
				return 0, pos, TrackInfo{}, ErrSlow
			}

			available := int(writePos - pos)
			n := min(len(p), available)
			track, nextTrackPos := rb.trackAt(pos)
			if nextTrackPos > pos && int64(n) > nextTrackPos-pos {
				n = int(nextTrackPos - pos)
			}

			start := int(pos) % rb.size
			if start+n <= rb.size {
				copy(p, rb.data[start:start+n])
			} else {
				first := rb.size - start
				copy(p, rb.data[start:start+first])
				copy(p[first:], rb.data[:n-first])
			}

			rb.rw.RUnlock()
			return n, pos + int64(n), track, nil
		}

		rb.notifMu.Lock()
		ch := rb.notif
		rb.notifMu.Unlock()
		if pos < rb.writePos.Load() {
			continue
		}

		select {
		case <-ch:
		case <-rb.closed:
			return 0, pos, TrackInfo{}, io.EOF
		case <-ctx.Done():
			return 0, pos, TrackInfo{}, ctx.Err()
		}
	}
}

func (rb *RingBuffer) trackAt(pos int64) (TrackInfo, int64) {
	var track TrackInfo
	for _, event := range rb.trackEvents {
		if event.pos > pos {
			return track, event.pos
		}
		track = event.track
	}
	return track, -1
}

// Close wakes blocked readers after the station pipeline has stopped.
func (rb *RingBuffer) Close() {
	rb.close.Do(func() {
		close(rb.closed)
	})
}
