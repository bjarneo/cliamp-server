package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cliamp-server/broadcast"
)

func TestStreamFlushesContinuousAudioPromptly(t *testing.T) {
	for _, wantMeta := range []bool{false, true} {
		t.Run(httpHeaderValue(wantMeta), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			hub := broadcast.NewHub("test", nil, 64, 0)
			recorder := newFlushRecorder()
			req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
			if wantMeta {
				req.Header.Set("Icy-MetaData", "1")
			}
			stream := &Stream{Hub: hub, MetaInt: 8192}
			done := make(chan struct{})
			go func() {
				stream.ServeHTTP(recorder, req)
				close(done)
			}()

			// The handler flushes headers before it starts streaming audio.
			select {
			case <-recorder.flushes:
			case <-time.After(time.Second):
				t.Fatal("stream did not flush response headers")
			}

			started := time.Now()
			frame := bytes.Repeat([]byte{1}, 417) // Typical MP3 frame at 128 kbps.
			for range 3 {
				hub.Ring().Write(frame)
				time.Sleep(26 * time.Millisecond)
			}

			select {
			case flushed := <-recorder.flushes:
				if elapsed := flushed.Sub(started); elapsed > 100*time.Millisecond {
					t.Errorf("audio flush after %v, want within 100ms", elapsed)
				}
			case <-time.After(150 * time.Millisecond):
				t.Fatal("continuous audio was not flushed within 150ms")
			}

			// RingBuffer.Read waits for a write, so wake it after cancelling the request.
			cancel()
			hub.Ring().Write(frame)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stream handler did not exit after request cancellation")
			}
		})
	}
}

func httpHeaderValue(wantMeta bool) string {
	if wantMeta {
		return "with_metadata"
	}
	return "without_metadata"
}

type flushRecorder struct {
	header  http.Header
	mu      sync.Mutex
	body    bytes.Buffer
	flushes chan time.Time
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		header:  make(http.Header),
		flushes: make(chan time.Time, 4),
	}
}

func (r *flushRecorder) Header() http.Header {
	return r.header
}

func (*flushRecorder) WriteHeader(int) {}

func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(p)
}

func (r *flushRecorder) Flush() {
	select {
	case r.flushes <- time.Now():
	default:
	}
}
