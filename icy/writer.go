package icy

import (
	"io"
)

// Writer interleaves audio data with ICY metadata blocks.
type Writer struct {
	w        io.Writer
	metaint  int
	sent     int    // Bytes sent since last metadata block
	meta     []byte // Current metadata block to insert
}

// NewWriter creates an ICY writer that injects metadata every metaint bytes.
func NewWriter(w io.Writer, metaint int) *Writer {
	return &Writer{
		w:       w,
		metaint: metaint,
		meta:    []byte{0x00}, // Start with empty metadata
	}
}

// SetMeta updates the metadata block to be inserted at the next boundary.
func (iw *Writer) SetMeta(title string) {
	iw.meta = BuildMeta(title)
}

// Write writes audio data, interleaving metadata blocks at metaint intervals.
func (iw *Writer) Write(p []byte) (int, error) {
	written := 0

	for len(p) > 0 {
		// How many audio bytes until next metadata insertion?
		remaining := iw.metaint - iw.sent

		if remaining > len(p) {
			// All of p fits before next metadata point
			n, err := iw.w.Write(p)
			iw.sent += n
			written += n
			return written, err
		}

		// Write audio bytes up to the metadata point
		if remaining > 0 {
			n, err := iw.w.Write(p[:remaining])
			iw.sent += n
			written += n
			if err != nil {
				return written, err
			}
			p = p[remaining:]
		}

		// Insert metadata block
		if _, err := iw.w.Write(iw.meta); err != nil {
			return written, err
		}
		iw.sent = 0
	}

	return written, nil
}

// WriteRaw writes audio data without metadata interleaving.
// Used for listeners that didn't request ICY metadata.
func WriteRaw(w io.Writer, p []byte) (int, error) {
	return w.Write(p)
}
