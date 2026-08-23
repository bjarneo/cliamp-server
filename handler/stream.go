package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
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

const streamWriteBufferSize = 4096

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
	wantMeta := r.Header.Get("Icy-MetaData") == "1"
	ctx := r.Context()
	ip := clientIP(r)

	// Reserve capacity before committing an audio response. Otherwise a full
	// station would append a text error to an already-started MP3 stream.
	info := broadcast.ListenerInfo{IP: ip}
	if s.GeoDB != nil {
		loc := s.GeoDB.Lookup(ip)
		info.Country = loc.Country
		info.CountryCode = loc.CountryCode
		info.City = loc.City
		info.Latitude = loc.Latitude
		info.Longitude = loc.Longitude
	}
	listener, err := s.Hub.AddListener(wantMeta, info)
	if err != nil {
		http.Error(w, "Server Full", http.StatusServiceUnavailable)
		return
	}
	defer s.Hub.RemoveListener(listener)

	h := w.Header()
	h.Set("Content-Type", "audio/mpeg")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("Connection", "close")
	// Go treats "identity" as a close-delimited response and removes this
	// sentinel from the wire. Raw ICY clients must not see HTTP chunk markers.
	h.Set("Transfer-Encoding", "identity")
	h.Set("icy-name", s.Name)
	h.Set("icy-genre", s.Genre)
	h.Set("icy-pub", "1")

	if s.URL != "" {
		h.Set("icy-url", s.URL)
	}
	bitrate, sampleRate := s.Hub.StreamProperties()
	if bitrate > 0 {
		h.Set("icy-br", strconv.Itoa(bitrate))
	}
	if sampleRate > 0 {
		h.Set("icy-sr", strconv.Itoa(sampleRate))
	}

	if wantMeta {
		h.Set("icy-metaint", strconv.Itoa(s.MetaInt))
	}

	w.WriteHeader(http.StatusOK)

	// Flush headers immediately
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

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

	// Stream audio from ring buffer
	ring := s.Hub.Ring()
	// The ring may advance while a per-listener intro plays. Join from a fresh,
	// frame-aligned preroll rather than the stale position reserved above.
	listener.SetPos(ring.PrerollPos())
	buf := make([]byte, streamWriteBufferSize)
	var lastTitle string

	flusher, _ := w.(http.Flusher)
	rc := http.NewResponseController(w)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, newPos, track, err := ring.Read(ctx, listener.Pos(), buf)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				slog.Debug("listener read error", "id", listener.ID, "error", err)
			}
			return
		}
		listener.SetPos(newPos)

		// Give slow clients a generous deadline, but don't block forever.
		writeTimeout := s.Hub.ListenerWriteTimeout(listener.Pos())
		if err := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return
		}

		// Update metadata if track changed
		if wantMeta {
			title := track.Title
			if track.Artist != "" {
				title = track.Artist + " - " + track.Title
			}
			if title != lastTitle {
				writer.SetMeta(title)
				lastTitle = title
			}

			if written, err := writer.Write(buf[:n]); err != nil || written != n {
				return
			}
		} else {
			if written, err := w.Write(buf[:n]); err != nil || written != n {
				return
			}
		}

		// Ring writes are frame-paced. Flush each read so a final frame cannot
		// remain buffered indefinitely when the producer pauses.
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// playIntro normalizes and streams the intro to the client at real-time speed.
// Returns true if intro completed (or was skipped due to error), false if client disconnected.
func (s *Stream) playIntro(ctx context.Context, w http.ResponseWriter, writer *icy.Writer) bool {
	src, err := transcode.NewReader(ctx, s.IntroFile)
	if err != nil {
		slog.Warn("cannot open intro file, skipping", "path", s.IntroFile, "error", err)
		return true
	}
	defer func() {
		if err := src.Close(); err != nil && ctx.Err() == nil {
			slog.Debug("cannot close intro decoder", "path", s.IntroFile, "error", err)
		}
	}()

	reader, err := mp3frame.NewReader(src)
	if err != nil {
		slog.Warn("cannot read intro file, skipping", "path", s.IntroFile, "error", err)
		return true
	}

	if writer != nil {
		writer.SetMeta("Station Intro")
	}

	flusher, _ := w.(http.Flusher)
	rc := http.NewResponseController(w)
	epoch := time.Now()
	totalSamples := int64(0)
	throttle := time.NewTimer(0)
	if !throttle.Stop() {
		<-throttle.C
	}
	defer throttle.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		frame, err := reader.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				slog.Warn("intro decode ended with an error", "path", s.IntroFile, "error", err)
			}
			if flusher != nil {
				flusher.Flush()
			}
			return true
		}

		if err := rc.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}

		if writer != nil {
			if written, err := writer.Write(frame.Data); err != nil || written != len(frame.Data) {
				return false
			}
		} else {
			if written, err := w.Write(frame.Data); err != nil || written != len(frame.Data) {
				return false
			}
		}

		if flusher != nil {
			flusher.Flush()
		}

		totalSamples += int64(frame.Samples)
		rate := int64(frame.SampleRate)
		elapsed := time.Duration(totalSamples/rate)*time.Second +
			time.Duration(totalSamples%rate)*time.Second/time.Duration(rate)
		deadline := epoch.Add(elapsed)
		if wait := time.Until(deadline); wait > 0 {
			throttle.Reset(wait)
			select {
			case <-throttle.C:
			case <-ctx.Done():
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
