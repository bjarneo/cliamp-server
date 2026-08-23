package icy

import "io"

// Writer interleaves audio data with ICY metadata blocks.
type Writer struct {
	w       io.Writer
	metaint int
	sent    int    // Bytes sent since last metadata block
	meta    []byte // Current metadata block to insert
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
		// Defer a boundary block until the next audio write. This lets callers
		// update the title when a track changes exactly at metaint.
		if iw.sent == iw.metaint {
			if _, err := writeFull(iw.w, iw.meta); err != nil {
				return written, err
			}
			iw.sent = 0
		}

		// How many audio bytes until next metadata insertion?
		remaining := iw.metaint - iw.sent
		next := min(len(p), remaining)
		n, err := writeFull(iw.w, p[:next])
		iw.sent += n
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
	}

	return written, nil
}

// WriteRaw writes audio data without metadata interleaving.
// Used for listeners that didn't request ICY metadata.
func WriteRaw(w io.Writer, p []byte) (int, error) {
	return writeFull(w, p)
}

func writeFull(w io.Writer, p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n, err := w.Write(p)
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
