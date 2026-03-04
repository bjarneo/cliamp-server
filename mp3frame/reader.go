package mp3frame

import (
	"fmt"
	"io"
)

// Reader reads MP3 frames from an underlying io.Reader.
type Reader struct {
	r   io.Reader
	buf []byte
}

// NewReader creates a frame reader. It skips any leading ID3v2 tag.
func NewReader(r io.Reader) (*Reader, error) {
	fr := &Reader{r: r, buf: make([]byte, 0, 8192)}

	if err := fr.skipID3v2(); err != nil {
		return nil, err
	}

	return fr, nil
}

// skipID3v2 checks for an ID3v2 header and skips it.
func (fr *Reader) skipID3v2() error {
	header := make([]byte, 10)
	_, err := io.ReadFull(fr.r, header)
	if err != nil {
		return fmt.Errorf("reading ID3v2 header: %w", err)
	}

	// Check for "ID3" magic
	if header[0] == 'I' && header[1] == 'D' && header[2] == '3' {
		// ID3v2 size is stored as synchsafe integer (4 bytes, 7 bits each)
		size := int(header[6])<<21 | int(header[7])<<14 | int(header[8])<<7 | int(header[9])
		// Check for footer flag (bit 4 of flags byte)
		if header[5]&0x10 != 0 {
			size += 10
		}
		if _, err := io.CopyN(io.Discard, fr.r, int64(size)); err != nil {
			return fmt.Errorf("skipping ID3v2 tag: %w", err)
		}
		return nil
	}

	// No ID3v2 tag — these bytes might be start of a frame.
	// Push them into a small buffer we'll consume first.
	fr.buf = append(fr.buf[:0], header...)
	return nil
}

// ReadFrame reads the next MP3 frame. Returns io.EOF at end of stream.
func (fr *Reader) ReadFrame() (Frame, error) {
	for {
		h, err := fr.findSync()
		if err != nil {
			return Frame{}, err
		}

		frame, err := ParseHeader(h)
		if err != nil {
			// Not a valid frame at this sync point; skip one byte and retry.
			continue
		}

		if frame.FrameSize < 4 || frame.FrameSize > 4608 {
			continue
		}

		// Read the rest of the frame
		data := make([]byte, frame.FrameSize)
		data[0] = h[0]
		data[1] = h[1]
		data[2] = h[2]
		data[3] = h[3]

		remaining := frame.FrameSize - 4
		if remaining > 0 {
			n, err := fr.read(data[4:])
			if err != nil {
				return Frame{}, err
			}
			if n < remaining {
				return Frame{}, io.ErrUnexpectedEOF
			}
		}

		frame.Data = data
		return frame, nil
	}
}

// findSync scans for an MP3 sync word (0xFFE0 mask) and returns the 4-byte header.
func (fr *Reader) findSync() ([4]byte, error) {
	var h [4]byte

	b, err := fr.readByte()
	if err != nil {
		return h, err
	}

	for {
		// Look for 0xFF
		for b != 0xFF {
			b, err = fr.readByte()
			if err != nil {
				return h, err
			}
		}

		h[0] = b
		b, err = fr.readByte()
		if err != nil {
			return h, err
		}

		if b&0xE0 == 0xE0 {
			h[1] = b
			// Read remaining 2 header bytes
			h[2], err = fr.readByte()
			if err != nil {
				return h, err
			}
			h[3], err = fr.readByte()
			if err != nil {
				return h, err
			}
			return h, nil
		}
		// Not a valid sync; b might be start of next 0xFF
	}
}

func (fr *Reader) readByte() (byte, error) {
	if len(fr.buf) > 0 {
		b := fr.buf[0]
		fr.buf = fr.buf[1:]
		return b, nil
	}

	var buf [1]byte
	_, err := io.ReadFull(fr.r, buf[:])
	return buf[0], err
}

func (fr *Reader) read(p []byte) (int, error) {
	n := 0

	// Drain buffered bytes first
	if len(fr.buf) > 0 {
		copied := copy(p, fr.buf)
		fr.buf = fr.buf[copied:]
		n += copied
		if n >= len(p) {
			return n, nil
		}
	}

	// Read the rest from underlying reader
	for n < len(p) {
		nn, err := fr.r.Read(p[n:])
		n += nn
		if err != nil {
			return n, err
		}
	}

	return n, nil
}
