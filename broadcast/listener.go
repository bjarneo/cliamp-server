package broadcast

import (
	"errors"
	"sync/atomic"
	"time"
)

// ErrSlow is returned when a listener has fallen too far behind the writer.
var ErrSlow = errors.New("listener too slow")

// ErrFull is returned when the listener limit has been reached.
var ErrFull = errors.New("listener limit reached")

// ListenerInfo holds connection metadata for a listener.
type ListenerInfo struct {
	IP          string
	Country     string
	CountryCode string
	City        string
	Latitude    float64
	Longitude   float64
}

// Listener represents a connected stream listener with its own read position.
type Listener struct {
	ID          int64
	Info        ListenerInfo
	ConnectedAt time.Time
	pos         int64         // Absolute read position in the ring buffer
	wantMeta    bool          // Client requested ICY metadata
	metaSent    int           // Bytes sent since last metadata block
	done        atomic.Bool   // Set when listener disconnects
}

// NewListener creates a listener starting at the given position.
func NewListener(id int64, pos int64, wantMeta bool, info ListenerInfo) *Listener {
	return &Listener{
		ID:          id,
		Info:        info,
		ConnectedAt: time.Now(),
		pos:         pos,
		wantMeta:    wantMeta,
	}
}

// Pos returns the listener's current read position.
func (l *Listener) Pos() int64 {
	return l.pos
}

// SetPos updates the listener's read position.
func (l *Listener) SetPos(pos int64) {
	l.pos = pos
}

// WantMeta returns whether this listener requested ICY metadata.
func (l *Listener) WantMeta() bool {
	return l.wantMeta
}

// MetaSent returns bytes sent since last metadata block.
func (l *Listener) MetaSent() int {
	return l.metaSent
}

// AddMetaSent adds to the byte counter since last metadata block.
func (l *Listener) AddMetaSent(n int) {
	l.metaSent += n
}

// ResetMetaSent resets the byte counter after sending a metadata block.
func (l *Listener) ResetMetaSent() {
	l.metaSent = 0
}

// Done returns true if the listener has disconnected.
func (l *Listener) Done() bool {
	return l.done.Load()
}

// Close marks the listener as done.
func (l *Listener) Close() {
	l.done.Store(true)
}
