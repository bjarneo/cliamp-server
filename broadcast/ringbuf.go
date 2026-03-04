package broadcast

import (
	"sync"
	"sync/atomic"
)

const prerollFrames = 128 // Frames of preroll (~3.3s at 128kbps) for new listeners — similar to Icecast burst-on-connect

// RingBuffer is a single-writer, multi-reader circular buffer with absolute positioning.
// Readers use a shared lock (RLock) so they can copy data concurrently, while the
// writer holds an exclusive lock. Notification uses a channel-close pattern instead
// of sync.Cond to avoid thundering-herd mutex contention on wake-up.
type RingBuffer struct {
	rw       sync.RWMutex
	data     []byte
	size     int
	writePos atomic.Int64

	// Channel-based notification replaces sync.Cond.  Closed on every Write
	// to wake all blocked readers; immediately replaced with a fresh channel.
	notifMu sync.Mutex
	notif   chan struct{}

	// Frame position history for listener preroll (only written under rw.Lock).
	frameHistory [prerollFrames]int64
	frameIdx     int
	frameCount   int
}

// NewRingBuffer creates a ring buffer of the given size in bytes.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data:  make([]byte, size),
		size:  size,
		notif: make(chan struct{}),
	}
}

// Write appends data to the ring buffer and wakes waiting readers.
// Each call is assumed to be one complete MP3 frame.
func (rb *RingBuffer) Write(p []byte) {
	rb.rw.Lock()

	pos := rb.writePos.Load()

	// Record this frame's start position
	rb.frameHistory[rb.frameIdx] = pos
	rb.frameIdx = (rb.frameIdx + 1) % prerollFrames
	if rb.frameCount < prerollFrames {
		rb.frameCount++
	}

	// Copy data to ring
	start := int(pos) % rb.size
	if start+len(p) <= rb.size {
		copy(rb.data[start:], p)
	} else {
		first := rb.size - start
		copy(rb.data[start:], p[:first])
		copy(rb.data, p[first:])
	}

	// Publish new write position AFTER data is fully written.
	rb.writePos.Add(int64(len(p)))

	rb.rw.Unlock()

	// Wake all blocked readers by closing the notification channel.
	rb.notifMu.Lock()
	close(rb.notif)
	rb.notif = make(chan struct{})
	rb.notifMu.Unlock()
}

// WritePos returns the current absolute write position.
func (rb *RingBuffer) WritePos() int64 {
	return rb.writePos.Load()
}

// PrerollPos returns a frame-aligned position several frames behind the write
// head. This gives new listeners enough prior data for MP3 bit reservoir decoding.
func (rb *RingBuffer) PrerollPos() int64 {
	rb.rw.RLock()
	defer rb.rw.RUnlock()

	if rb.frameCount == 0 {
		return 0
	}

	// Get the oldest frame start in the history
	oldest := (rb.frameIdx - rb.frameCount + prerollFrames) % prerollFrames
	pos := rb.frameHistory[oldest]

	// If that position has been overwritten, fall back to the most recent frame
	if rb.writePos.Load()-pos > int64(rb.size) {
		recent := (rb.frameIdx - 1 + prerollFrames) % prerollFrames
		return rb.frameHistory[recent]
	}

	return pos
}

// Read reads up to len(p) bytes starting at the given absolute position.
// It blocks until data is available. Returns the number of bytes read and the new position.
// If the reader has fallen too far behind (overwritten), returns ErrSlow.
//
// Multiple readers execute concurrently under a shared read-lock; the only
// exclusive section is the writer's memcpy + position update.
func (rb *RingBuffer) Read(pos int64, p []byte) (int, int64, error) {
	for {
		wp := rb.writePos.Load()
		if pos < wp {
			// Data available — take read lock for concurrent copy.
			rb.rw.RLock()

			// Re-check under lock: writer may have wrapped past us.
			wp = rb.writePos.Load()
			if wp-pos > int64(rb.size) {
				rb.rw.RUnlock()
				return 0, 0, ErrSlow
			}

			available := int(wp - pos)
			n := len(p)
			if n > available {
				n = available
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
			return n, pos + int64(n), nil
		}

		// No data yet — grab the notification channel, then double-check.
		rb.notifMu.Lock()
		ch := rb.notif
		rb.notifMu.Unlock()

		if pos < rb.writePos.Load() {
			continue // data arrived between check and channel acquisition
		}

		<-ch // block until next Write
	}
}
