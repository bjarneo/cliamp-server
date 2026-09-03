package handler

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stream handler did not exit after request cancellation")
			}
		})
	}
}

func TestStreamUsesCloseDelimitedHTTP1(t *testing.T) {
	hub := broadcast.NewHub("test", nil, 64, 0)
	frame := bytes.Repeat([]byte{0x7f}, 417)
	track := broadcast.TrackInfo{Title: "Test"}
	hub.Ring().WriteFrame(frame, &track)
	stream := &Stream{Hub: hub, MetaInt: 8192, Name: "Test"}
	server := httptest.NewServer(stream)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")
	conn, err := net.DialTimeout("tcp", host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}

	request := "GET / HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, " 200 ") {
		t.Fatalf("status line = %q, want 200", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "transfer-encoding:") {
			t.Fatalf("unexpected HTTP transfer framing: %q", line)
		}
	}

	body := make([]byte, len(frame))
	if _, err := io.ReadFull(reader, body); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, frame) {
		t.Fatalf("first body bytes contain HTTP framing")
	}
}

func TestStreamRejectsFullStationBeforeStartingAudio(t *testing.T) {
	hub := broadcast.NewHub("test", nil, 64, 1)
	held, err := hub.AddListener(false, broadcast.ListenerInfo{})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.RemoveListener(held)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	stream := &Stream{Hub: hub, MetaInt: 8192}
	stream.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Body.String(); got != "Server Full\n" {
		t.Fatalf("body = %q, want a plain capacity error", got)
	}
}

func TestStreamIncludesLogoHeader(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	hub := broadcast.NewHub("test", nil, 64, 0)
	recorder := newFlushRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://radio.example/omarchy/stream", nil).WithContext(ctx)
	req.Header.Set("X-Forwarded-Proto", "https")
	stream := &Stream{Hub: hub, MetaInt: 8192}
	done := make(chan struct{})
	go func() {
		stream.ServeHTTP(recorder, req)
		close(done)
	}()

	select {
	case <-recorder.flushes:
	case <-time.After(time.Second):
		t.Fatal("stream did not flush response headers")
	}
	if got, want := recorder.Header().Get("Icy-Logo"), "https://radio.example/logo.svg"; got != want {
		t.Errorf("Icy-Logo = %q, want %q", got, want)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not exit after request cancellation")
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
