package handler

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cliamp-server/broadcast"
	"cliamp-server/geo"
	"cliamp-server/icy"
	"cliamp-server/mp3frame"
	"cliamp-server/transcode"
)

// introSeen tracks which IPs have heard which stream's intro.
// Key: "streamName:IP", Value: time.Time of last intro play.
var introSeen sync.Map

const (
	streamWriteBufferSize = 4096
	streamFlushBytes      = 1024
	streamFlushInterval   = 50 * time.Millisecond
)

// Stream handles GET /stream — the main audio stream endpoint.
type Stream struct {
	Hub          *broadcast.Hub
	StationID    string // Unique station identifier (TOML key, e.g. "pop", "jazz")
	MetaInt      int
	Name         string
	Genre        string
	URL          string
	IntroFile    string        // Path to intro MP3 (empty = no intro)
	GeoDB        *geo.DB       // Optional MaxMind geo database (nil = no geo lookup)
	IntroSeenTTL time.Duration // How long to suppress intro replays (0 = 48h default)
}

func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if client wants ICY metadata
	wantMeta := r.Header.Get("Icy-MetaData") == "1"

	// Set response headers
	h := w.Header()
	h.Set("Content-Type", "audio/mpeg")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("Connection", "close")
	h.Set("icy-name", s.Name)
	h.Set("icy-genre", s.Genre)
	h.Set("icy-pub", "1")

	if s.URL != "" {
		h.Set("icy-url", s.URL)
	}
	if s.Hub.Bitrate > 0 {
		h.Set("icy-br", strconv.Itoa(s.Hub.Bitrate))
	}
	if s.Hub.SampleRate > 0 {
		h.Set("icy-sr", strconv.Itoa(s.Hub.SampleRate))
	}

	if wantMeta {
		h.Set("icy-metaint", strconv.Itoa(s.MetaInt))
	}

	w.WriteHeader(http.StatusOK)

	// Flush headers immediately
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ctx := r.Context()
	ip := clientIP(r)

	// Create ICY writer (reused across intro + broadcast for correct byte alignment)
	var writer *icy.Writer
	if wantMeta {
		writer = icy.NewWriter(w, s.MetaInt)
	}

	// Play intro only on the initial connection — skip for clients that
	// have already heard it (e.g. pause/resume causing a reconnect).
	// Tracked per stream so switching stations still plays that station's intro.
	// The suppression expires after IntroSeenTTL (default 48 hours).
	if s.IntroFile != "" {
		ttl := s.IntroSeenTTL
		if ttl == 0 {
			ttl = 48 * time.Hour
		}
		key := s.StationID + ":" + ip
		playIntro := true
		if v, ok := introSeen.Load(key); ok {
			if time.Since(v.(time.Time)) < ttl {
				playIntro = false
			}
		}
		if playIntro {
			if !s.playIntro(ctx, w, writer) {
				return // client disconnected during intro
			}
			introSeen.Store(key, time.Now())
		}
	}

	// Build listener info from client IP and optional geo lookup
	info := broadcast.ListenerInfo{IP: ip}
	if s.GeoDB != nil {
		loc := s.GeoDB.Lookup(ip)
		info.Country = loc.Country
		info.CountryCode = loc.CountryCode
		info.City = loc.City
		info.Latitude = loc.Latitude
		info.Longitude = loc.Longitude
	}

	// Register listener and join live broadcast
	listener, err := s.Hub.AddListener(wantMeta, info)
	if err != nil {
		http.Error(w, "Server Full", http.StatusServiceUnavailable)
		return
	}
	defer s.Hub.RemoveListener(listener)

	// Stream audio from ring buffer
	ring := s.Hub.Ring()
	buf := make([]byte, streamWriteBufferSize)
	var lastTitle string

	flusher, _ := w.(http.Flusher)
	rc := http.NewResponseController(w)

	// Batch writes instead of flushing every MP3 frame, while keeping the
	// delivery cadence below typical client output-buffer sizes. At 128 kbps,
	// 1 KB is about 64 ms of audio.
	unflushed := 0
	lastFlush := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, newPos, err := ring.Read(listener.Pos(), buf)
		if err != nil {
			slog.Debug("listener read error", "id", listener.ID, "error", err)
			return
		}
		listener.SetPos(newPos)

		// Give slow clients a generous deadline, but don't block forever.
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))

		// Update metadata if track changed
		if wantMeta {
			track := s.Hub.CurrentTrack()
			title := track.Title
			if track.Artist != "" {
				title = track.Artist + " - " + track.Title
			}
			if title != lastTitle {
				writer.SetMeta(title)
				lastTitle = title
			}

			if _, err := writer.Write(buf[:n]); err != nil {
				return
			}
		} else {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
		}

		unflushed += n
		if flusher != nil && (unflushed >= streamFlushBytes || time.Since(lastFlush) >= streamFlushInterval) {
			flusher.Flush()
			unflushed = 0
			lastFlush = time.Now()
		}
	}
}

// playIntro streams the intro MP3 directly to the client at real-time speed.
// Returns true if intro completed (or was skipped due to error), false if client disconnected.
func (s *Stream) playIntro(ctx context.Context, w http.ResponseWriter, writer *icy.Writer) bool {
	var (
		src io.ReadCloser
		err error
	)
	if transcode.NeedsTranscode(s.IntroFile) {
		src, err = transcode.NewReader(ctx, s.IntroFile)
	} else {
		src, err = os.Open(s.IntroFile)
	}
	if err != nil {
		slog.Warn("cannot open intro file, skipping", "path", s.IntroFile, "error", err)
		return true
	}
	defer src.Close()

	reader, err := mp3frame.NewReader(src)
	if err != nil {
		slog.Warn("cannot read intro file, skipping", "path", s.IntroFile, "error", err)
		return true
	}

	if writer != nil {
		writer.SetMeta("Station Intro")
	}

	// No real-time throttle for the intro: TCP backpressure naturally
	// rate-limits delivery, and blasting frames gets the listener to the
	// live broadcast faster.  Per-frame Flush is unnecessary here — the
	// tight read loop fills the 4KB ResponseWriter buffer in microseconds,
	// triggering auto-flush.
	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		frame, err := reader.ReadFrame()
		if err != nil {
			// Flush any remaining intro data before joining live broadcast.
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return true // end of intro or read error — proceed to broadcast
		}

		// Write frame to client
		if writer != nil {
			if _, err := writer.Write(frame.Data); err != nil {
				return false
			}
		} else {
			if _, err := w.Write(frame.Data); err != nil {
				return false
			}
		}
	}
}

// clientIP extracts the client's IP address from the request,
// checking X-Forwarded-For and X-Real-IP before falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first (leftmost) IP — the original client
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// RemoteAddr is "host:port"; strip the port
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
