package mp3frame

import "fmt"

// Frame represents a parsed MP3 frame header plus its raw bytes.
type Frame struct {
	Header     [4]byte
	Data       []byte // Full frame bytes including header
	Version    int    // 1=MPEG1, 2=MPEG2, 25=MPEG2.5
	Layer      int    // Layer III (3)
	Bitrate    int    // kbps
	SampleRate int    // Hz
	Padding    bool
	FrameSize  int // Total frame size in bytes
	Samples    int // Samples per frame
}

// MPEG1 Layer III bitrate table (index 0 = free, index 15 = bad)
var bitrateTable = [16]int{
	0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0,
}

// MPEG2/2.5 Layer III bitrate table
var bitrateTableV2 = [16]int{
	0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0,
}

var sampleRateTable = [4][4]int{
	// MPEG2.5
	{11025, 12000, 8000, 0},
	// reserved
	{0, 0, 0, 0},
	// MPEG2
	{22050, 24000, 16000, 0},
	// MPEG1
	{44100, 48000, 32000, 0},
}

// ParseHeader parses a 4-byte MP3 frame header.
func ParseHeader(h [4]byte) (Frame, error) {
	// Check sync word: 11 bits set (0xFFE0)
	if h[0] != 0xFF || (h[1]&0xE0) != 0xE0 {
		return Frame{}, fmt.Errorf("invalid sync word")
	}

	f := Frame{Header: h}

	// Version: bits 4-3 of byte 1
	versionBits := (h[1] >> 3) & 0x03
	switch versionBits {
	case 0:
		f.Version = 25 // MPEG 2.5
	case 2:
		f.Version = 2 // MPEG 2
	case 3:
		f.Version = 1 // MPEG 1
	default:
		return Frame{}, fmt.Errorf("reserved MPEG version")
	}

	// Layer: bits 2-1 of byte 1. The bitrate tables and streaming pipeline
	// intentionally support Layer III only.
	layerBits := (h[1] >> 1) & 0x03
	if layerBits == 0 {
		return Frame{}, fmt.Errorf("reserved layer")
	}
	if layerBits != 1 {
		return Frame{}, fmt.Errorf("unsupported MPEG layer: %d", 4-layerBits)
	}
	f.Layer = 3

	// Bitrate: bits 7-4 of byte 2
	brIndex := (h[2] >> 4) & 0x0F
	if f.Version == 1 {
		f.Bitrate = bitrateTable[brIndex]
	} else {
		f.Bitrate = bitrateTableV2[brIndex]
	}
	if f.Bitrate == 0 {
		return Frame{}, fmt.Errorf("invalid bitrate index: %d", brIndex)
	}

	// Sample rate: bits 3-2 of byte 2
	srIndex := (h[2] >> 2) & 0x03
	f.SampleRate = sampleRateTable[versionBits][srIndex]
	if f.SampleRate == 0 {
		return Frame{}, fmt.Errorf("invalid sample rate")
	}

	// Padding: bit 1 of byte 2
	f.Padding = (h[2]>>1)&0x01 == 1

	// Samples per frame
	if f.Version != 1 {
		f.Samples = 576 // MPEG2/2.5 Layer III
	} else {
		f.Samples = 1152
	}

	// Frame size calculation
	pad := 0
	if f.Padding {
		pad = 1
	}

	if f.Version == 1 {
		f.FrameSize = 144*f.Bitrate*1000/f.SampleRate + pad
	} else {
		f.FrameSize = 72*f.Bitrate*1000/f.SampleRate + pad
	}

	return f, nil
}
