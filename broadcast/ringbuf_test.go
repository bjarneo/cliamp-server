package broadcast

import (
	"sync"
	"testing"

	"cliamp-server/library"
)

func TestRingBufferConcurrentReaders(t *testing.T) {
	rb := NewRingBuffer(64 * 1024)

	const (
		numReaders = 100
		numFrames  = 500
		frameSize  = 417 // typical MP3 frame at 128kbps
	)

	// Write a frame to get an initial position.
	frame := make([]byte, frameSize)
	for i := range frame {
		frame[i] = byte(i % 256)
	}
	rb.Write(frame)

	startPos := rb.PrerollPos()

	var wg sync.WaitGroup

	// Spawn concurrent readers that each consume all frames.
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4096)
			pos := startPos
			totalRead := 0

			for totalRead < numFrames*frameSize {
				n, newPos, err := rb.Read(pos, buf)
				if err != nil {
					t.Errorf("read error at pos %d: %v", pos, err)
					return
				}
				pos = newPos
				totalRead += n
			}
		}()
	}

	// Writer: produce frames at full speed.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numFrames; i++ {
			rb.Write(frame)
		}
	}()

	wg.Wait()
}

func TestRingBufferErrFull(t *testing.T) {
	source := &fakeSource{}
	hub := NewHub("test", source, 64, 2)

	info := ListenerInfo{IP: "127.0.0.1"}

	l1, err := hub.AddListener(false, info)
	if err != nil {
		t.Fatalf("first AddListener failed: %v", err)
	}

	l2, err := hub.AddListener(false, info)
	if err != nil {
		t.Fatalf("second AddListener failed: %v", err)
	}

	// Third should fail.
	_, err = hub.AddListener(false, info)
	if err != ErrFull {
		t.Fatalf("expected ErrFull, got %v", err)
	}

	// Remove one and try again.
	hub.RemoveListener(l1)

	l3, err := hub.AddListener(false, info)
	if err != nil {
		t.Fatalf("AddListener after remove failed: %v", err)
	}

	hub.RemoveListener(l2)
	hub.RemoveListener(l3)
}

// fakeSource satisfies playlist.TrackSource for tests.
type fakeSource struct{}

func (f *fakeSource) Next() library.Track {
	return library.Track{Path: "test.mp3", Title: "Test"}
}
