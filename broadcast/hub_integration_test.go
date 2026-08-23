package broadcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cliamp-server/library"
)

func TestHubContinuousEncoderHasNoTrackBoundarySilence(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}

	dir := t.TempDir()
	tracks := make([]library.Track, 2)
	for i := range tracks {
		path := filepath.Join(dir, fmt.Sprintf("tone-%d.wav", i))
		sampleRate, channels := 44100, 2
		if i == 1 {
			sampleRate, channels = 48000, 1
		}
		writeToneWAV(t, path, 997, sampleRate, channels, time.Second)
		tracks[i] = library.Track{Path: path, Title: fmt.Sprintf("Tone %d", i)}
	}

	source := &cyclicTrackSource{tracks: tracks}
	hub := NewHub("test", source, 512, 0)
	runCtx, cancelRun := context.WithCancel(t.Context())
	runDone := make(chan struct{})
	go func() {
		hub.Run(runCtx)
		close(runDone)
	}()

	captureCtx, cancelCapture := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelCapture()
	var encoded bytes.Buffer
	buf := make([]byte, 8192)
	pos := int64(0)
	for encoded.Len() < 50_000 {
		n, nextPos, _, err := hub.Ring().Read(captureCtx, pos, buf)
		if err != nil {
			cancelRun()
			t.Fatalf("capture station audio: %v", err)
		}
		encoded.Write(buf[:n])
		pos = nextPos
	}

	cancelRun()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("hub did not stop after cancellation")
	}

	decodeCtx, cancelDecode := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelDecode()
	cmd := exec.CommandContext(decodeCtx, ffmpeg,
		"-hide_banner",
		"-loglevel", "error",
		"-f", "mp3",
		"-i", "pipe:0",
		"-map", "0:a:0",
		"-ac", "1",
		"-ar", "44100",
		"-f", "s16le",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(encoded.Bytes())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pcm, err := cmd.Output()
	if err != nil {
		t.Fatalf("decode captured stream: %v: %s", err, stderr.String())
	}

	const (
		sampleRate = 44100
		window     = sampleRate / 100 // 10 ms
		threshold  = 500.0
	)
	sampleCount := len(pcm) / 2
	if sampleCount < 2*sampleRate {
		t.Fatalf("decoded only %.2f seconds of audio", float64(sampleCount)/sampleRate)
	}

	start := sampleRate / 4
	end := sampleCount - sampleRate/4
	for offset := start; offset+window <= end; offset += window {
		var sum float64
		for i := offset; i < offset+window; i++ {
			sample := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
			sum += float64(sample) * float64(sample)
		}
		rms := math.Sqrt(sum / window)
		if rms < threshold {
			t.Fatalf("detected a silent 10 ms window at %.3f seconds (RMS %.1f)", float64(offset)/sampleRate, rms)
		}
	}
}

type cyclicTrackSource struct {
	mu     sync.Mutex
	tracks []library.Track
	next   int
}

func (s *cyclicTrackSource) Next() library.Track {
	s.mu.Lock()
	defer s.mu.Unlock()
	track := s.tracks[s.next]
	s.next = (s.next + 1) % len(s.tracks)
	return track
}

func writeToneWAV(t *testing.T, path string, frequency int, sampleRate int, channels int, duration time.Duration) {
	t.Helper()
	const (
		bytesPerSample = 2
		bitsPerSample  = bytesPerSample * 8
	)
	samples := int64(duration) * int64(sampleRate) / int64(time.Second)
	dataSize := samples * int64(channels*bytesPerSample)

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close WAV fixture: %v", err)
		}
	}()

	write := func(value any) {
		t.Helper()
		if err := binary.Write(file, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := file.WriteString("RIFF"); err != nil {
		t.Fatal(err)
	}
	write(uint32(36 + dataSize))
	if _, err := file.WriteString("WAVEfmt "); err != nil {
		t.Fatal(err)
	}
	write(uint32(16))
	write(uint16(1))
	write(uint16(channels))
	write(uint32(sampleRate))
	write(uint32(sampleRate * channels * bytesPerSample))
	write(uint16(channels * bytesPerSample))
	write(uint16(bitsPerSample))
	if _, err := file.WriteString("data"); err != nil {
		t.Fatal(err)
	}
	write(uint32(dataSize))

	data := make([]byte, int(dataSize))
	offset := 0
	for i := int64(0); i < samples; i++ {
		value := int16(math.Sin(2*math.Pi*float64(frequency)*float64(i)/float64(sampleRate)) * 12000)
		for range channels {
			binary.LittleEndian.PutUint16(data[offset:], uint16(value))
			offset += bytesPerSample
		}
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
}
