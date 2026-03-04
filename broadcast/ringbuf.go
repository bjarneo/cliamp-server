package broadcast

import (
	"sync"
)

const prerollFrames = 128 // Frames of preroll (~3.3s at 128kbps) for new listeners — similar to Icecast burst-on-connect

// RingBuffer is a single-writer, multi-reader circular buffer with absolute positioning.
type RingBuffer struct {
	mu       sync.Mutex
	cond     *sync.Cond
	data     []byte
	size     int
	writePos int64 // Absolute write position (monotonically increasing)

	// Frame position history for listener preroll.
	// MP3 Layer III uses a "bit reservoir" where frames reference data from
	// prior frames. New listeners need several prior frames so the decoder
	// can fill the reservoir before producing audio.
	frameHistory [prerollFrames]int64 // ring of frame start positions
	frameIdx     int
	frameCount   int
}

// NewRingBuffer creates a ring buffer of the given size in bytes.
func NewRingBuffer(size int) *RingBuffer {
	rb := &RingBuffer{
		data: make([]byte, size),
		size: size,
	}
	rb.cond = sync.NewCond(&rb.mu)
	return rb
}

// Write appends data to the ring buffer and wakes waiting readers.
// Each call is assumed to be one complete MP3 frame.
func (rb *RingBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Record this frame's start position
	rb.frameHistory[rb.frameIdx] = rb.writePos
	rb.frameIdx = (rb.frameIdx + 1) % prerollFrames
	if rb.frameCount < prerollFrames {
		rb.frameCount++
	}

	start := int(rb.writePos) % rb.size
	if start+len(p) <= rb.size {
		copy(rb.data[start:], p)
	} else {
		first := rb.size - start
		copy(rb.data[start:], p[:first])
		copy(rb.data, p[first:])
	}
	rb.writePos += int64(len(p))
	rb.cond.Broadcast()
}

// WritePos returns the current absolute write position.
func (rb *RingBuffer) WritePos() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.writePos
}

// PrerollPos returns a frame-aligned position several frames behind the write
// head. This gives new listeners enough prior data for MP3 bit reservoir decoding.
func (rb *RingBuffer) PrerollPos() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.frameCount == 0 {
		return 0
	}

	// Get the oldest frame start in the history
	oldest := (rb.frameIdx - rb.frameCount + prerollFrames) % prerollFrames
	pos := rb.frameHistory[oldest]

	// If that position has been overwritten, fall back to the most recent frame
	if rb.writePos-pos > int64(rb.size) {
		recent := (rb.frameIdx - 1 + prerollFrames) % prerollFrames
		return rb.frameHistory[recent]
	}

	return pos
}

// Read reads up to len(p) bytes starting at the given absolute position.
// It blocks until data is available. Returns the number of bytes read and the new position.
// If the reader has fallen too far behind (overwritten), returns ErrSlow.
func (rb *RingBuffer) Read(pos int64, p []byte) (int, int64, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Wait until there's data to read
	for pos >= rb.writePos {
		rb.cond.Wait()
	}

	// Check if reader position has been overwritten
	if rb.writePos-pos > int64(rb.size) {
		return 0, 0, ErrSlow
	}

	// Calculate how much we can read
	available := int(rb.writePos - pos)
	n := len(p)
	if n > available {
		n = available
	}

	// Copy data from ring buffer
	start := int(pos) % rb.size
	if start+n <= rb.size {
		copy(p, rb.data[start:start+n])
	} else {
		first := rb.size - start
		copy(p, rb.data[start:start+first])
		copy(p[first:], rb.data[:n-first])
	}

	return n, pos + int64(n), nil
}
