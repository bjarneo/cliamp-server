package broadcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"cliamp-server/library"
)

func TestRingBufferConcurrentReaders(t *testing.T) {
	const (
		numReaders       = 100
		additionalFrames = 500
		totalFrames      = additionalFrames + 1
		frameSize        = 417 // typical MP3 frame at 128kbps
	)
	rb := NewRingBuffer(totalFrames * frameSize)

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

			for totalRead < totalFrames*frameSize {
				n, newPos, _, err := rb.Read(t.Context(), pos, buf)
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
		for i := 0; i < additionalFrames; i++ {
			rb.Write(frame)
		}
	}()

	wg.Wait()
}

func TestRingBufferPrerollUsesOldestRetainedFrame(t *testing.T) {
	rb := NewRingBuffer(50)
	for range 10 {
		rb.Write(make([]byte, 10))
	}

	if got := rb.PrerollPos(); got != 50 {
		t.Fatalf("PrerollPos() = %d, want 50", got)
	}
}

func TestRingBufferReadStopsAtTrackTransition(t *testing.T) {
	rb := NewRingBuffer(64)
	first := TrackInfo{Title: "First"}
	second := TrackInfo{Title: "Second"}
	rb.WriteFrame([]byte("aaaa"), &first)
	rb.Write([]byte("bbbb"))
	rb.WriteFrame([]byte("cccc"), &second)

	buf := make([]byte, 32)
	n, pos, track, err := rb.Read(t.Context(), 0, buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 8 || pos != 8 || track != first || !bytes.Equal(buf[:n], []byte("aaaabbbb")) {
		t.Fatalf("first read = (%d, %d, %+v, %q), want (8, 8, %+v, %q)", n, pos, track, buf[:n], first, "aaaabbbb")
	}

	n, pos, track, err = rb.Read(t.Context(), pos, buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || pos != 12 || track != second || !bytes.Equal(buf[:n], []byte("cccc")) {
		t.Fatalf("second read = (%d, %d, %+v, %q), want (4, 12, %+v, %q)", n, pos, track, buf[:n], second, "cccc")
	}
}

func TestRingBufferReadCanBeCanceled(t *testing.T) {
	rb := NewRingBuffer(64)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, _, err := rb.Read(ctx, 0, make([]byte, 16))
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not return after cancellation")
	}
}

func TestRingBufferCloseWakesReader(t *testing.T) {
	rb := NewRingBuffer(64)
	done := make(chan error, 1)
	go func() {
		_, _, _, err := rb.Read(t.Context(), 0, make([]byte, 16))
		done <- err
	}()

	rb.Close()
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Read() error = %v, want io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not return after close")
	}
}

func TestHubListenerWriteTimeoutFitsRingRetention(t *testing.T) {
	tests := []struct {
		name         string
		bufferSizeKB int
		want         time.Duration
	}{
		{name: "minimum buffer", bufferSizeKB: 64, want: 3596 * time.Millisecond},
		{name: "default buffer", bufferSizeKB: 512, want: 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := NewHub("test", nil, tt.bufferSizeKB, 0)
			if got := hub.ListenerWriteTimeout(0); got != tt.want {
				t.Fatalf("ListenerWriteTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHubListenerWriteTimeoutAccountsForPrerollLag(t *testing.T) {
	hub := NewHub("test", nil, 64, 0)
	frame := make([]byte, 417)
	for range prerollFrames {
		hub.ring.Write(frame)
	}

	if got, want := hub.ListenerWriteTimeout(hub.ring.PrerollPos()), 260*time.Millisecond; got != want {
		t.Fatalf("ListenerWriteTimeout() = %v, want %v", got, want)
	}
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

func TestHubListenerChangeHook(t *testing.T) {
	hub := NewHub("test", &fakeSource{}, 64, 2)
	var changes []int
	hub.SetListenerChangeHook(func(stationID string, delta int) {
		if stationID != "test" {
			t.Errorf("station ID = %q, want test", stationID)
		}
		changes = append(changes, delta)
	})

	listener, err := hub.AddListener(false, ListenerInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.AddListener(false, ListenerInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.AddListener(false, ListenerInfo{}); err != ErrFull {
		t.Fatalf("AddListener() error = %v, want %v", err, ErrFull)
	}

	hub.RemoveListener(listener)
	hub.RemoveListener(listener)

	want := []int{1, 1, -1}
	if len(changes) != len(want) {
		t.Fatalf("listener changes = %v, want %v", changes, want)
	}
	for i, change := range changes {
		if change != want[i] {
			t.Errorf("listener change %d = %d, want %d", i, change, want[i])
		}
	}
}

// fakeSource satisfies playlist.TrackSource for tests.
type fakeSource struct{}

func (f *fakeSource) Next() library.Track {
	return library.Track{Path: "test.mp3", Title: "Test"}
}
