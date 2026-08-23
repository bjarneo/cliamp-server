package mp3frame

import (
	"bufio"
	"fmt"
	"io"
)

const readerBufferSize = 8192

// Reader reads MP3 frames from an underlying io.Reader.
type Reader struct {
	r *bufio.Reader
}

// NewReader creates a buffered frame reader. It skips any leading ID3v2 tag.
func NewReader(r io.Reader) (*Reader, error) {
	fr := &Reader{r: bufio.NewReaderSize(r, readerBufferSize)}
	if err := fr.skipID3v2(); err != nil {
		return nil, err
	}
	return fr, nil
}

func (fr *Reader) skipID3v2() error {
	header, err := fr.r.Peek(10)
	if err != nil {
		return fmt.Errorf("reading ID3v2 header: %w", err)
	}
	if header[0] != 'I' || header[1] != 'D' || header[2] != '3' {
		return nil
	}

	for _, b := range header[6:10] {
		if b&0x80 != 0 {
			return fmt.Errorf("invalid ID3v2 size")
		}
	}

	size := int64(header[6])<<21 | int64(header[7])<<14 | int64(header[8])<<7 | int64(header[9])
	if header[5]&0x10 != 0 {
		size += 10
	}
	if _, err := fr.r.Discard(10); err != nil {
		return fmt.Errorf("discarding ID3v2 header: %w", err)
	}
	if _, err := io.CopyN(io.Discard, fr.r, size); err != nil {
		return fmt.Errorf("skipping ID3v2 tag: %w", err)
	}
	return nil
}

// ReadFrame reads the next MP3 frame. Invalid candidates advance by one byte,
// preserving sync words that begin inside the previous four-byte window.
func (fr *Reader) ReadFrame() (Frame, error) {
	for {
		header, err := fr.r.Peek(4)
		if err != nil {
			return Frame{}, err
		}

		var h [4]byte
		copy(h[:], header)
		frame, err := ParseHeader(h)
		if err != nil || frame.FrameSize < 4 || frame.FrameSize > 4608 {
			if _, discardErr := fr.r.Discard(1); discardErr != nil {
				return Frame{}, discardErr
			}
			continue
		}

		data := make([]byte, frame.FrameSize)
		if _, err := io.ReadFull(fr.r, data); err != nil {
			return Frame{}, err
		}
		frame.Data = data
		return frame, nil
	}
}
