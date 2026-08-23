package mp3frame

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestParseHeaderLayerIIIFrameSizes(t *testing.T) {
	tests := []struct {
		name       string
		header     [4]byte
		version    int
		bitrate    int
		sampleRate int
		frameSize  int
		samples    int
	}{
		{name: "MPEG-1", header: [4]byte{0xff, 0xfb, 0x90, 0}, version: 1, bitrate: 128, sampleRate: 44100, frameSize: 417, samples: 1152},
		{name: "MPEG-1 padded", header: [4]byte{0xff, 0xfb, 0x92, 0}, version: 1, bitrate: 128, sampleRate: 44100, frameSize: 418, samples: 1152},
		{name: "MPEG-2", header: [4]byte{0xff, 0xf3, 0x80, 0}, version: 2, bitrate: 64, sampleRate: 22050, frameSize: 208, samples: 576},
		{name: "MPEG-2 padded", header: [4]byte{0xff, 0xf3, 0x82, 0}, version: 2, bitrate: 64, sampleRate: 22050, frameSize: 209, samples: 576},
		{name: "MPEG-2.5", header: [4]byte{0xff, 0xe3, 0x40, 0}, version: 25, bitrate: 32, sampleRate: 11025, frameSize: 208, samples: 576},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := ParseHeader(tt.header)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Version != tt.version || frame.Bitrate != tt.bitrate || frame.SampleRate != tt.sampleRate || frame.FrameSize != tt.frameSize || frame.Samples != tt.samples {
				t.Fatalf("ParseHeader() = version %d, %d kbps, %d Hz, %d bytes, %d samples; want %d, %d, %d, %d, %d", frame.Version, frame.Bitrate, frame.SampleRate, frame.FrameSize, frame.Samples, tt.version, tt.bitrate, tt.sampleRate, tt.frameSize, tt.samples)
			}
		})
	}
}

func TestParseHeaderRejectsOtherLayers(t *testing.T) {
	if _, err := ParseHeader([4]byte{0xff, 0xfd, 0x80, 0}); err == nil {
		t.Fatal("ParseHeader accepted MPEG Layer II")
	}
}

func TestReaderRecoversSyncInsideInvalidCandidate(t *testing.T) {
	header := [4]byte{0xff, 0xfb, 0x90, 0}
	frame := testFrame(t, header, 0x5a)
	input := append([]byte{0xff, 0xe0, 0x00}, frame...)
	input = append(input, frame...)

	reader, err := NewReader(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		got, err := reader.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		if !bytes.Equal(got.Data, frame) {
			t.Fatalf("ReadFrame %d returned corrupt frame", i)
		}
	}
	if _, err := reader.ReadFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("final ReadFrame error = %v, want io.EOF", err)
	}
}

func TestReaderSkipsID3v2(t *testing.T) {
	header := [4]byte{0xff, 0xfb, 0x90, 0}
	frame := testFrame(t, header, 0x33)
	id3 := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 4, 't', 'a', 'g', '!'}

	reader, err := NewReader(bytes.NewReader(append(id3, frame...)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, frame) {
		t.Fatal("ReadFrame returned corrupt data after ID3v2 tag")
	}
}

func testFrame(t *testing.T, header [4]byte, fill byte) []byte {
	t.Helper()
	parsed, err := ParseHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	frame := bytes.Repeat([]byte{fill}, parsed.FrameSize)
	copy(frame, header[:])
	return frame
}
