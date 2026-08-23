package broadcast

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"cliamp-server/library"
	"cliamp-server/mp3frame"
	"cliamp-server/playlist"
	"cliamp-server/transcode"
)

const (
	pcmPrefetchBytes      = transcode.OutputSampleRate * transcode.PCMFrameBytes / 2
	decoderRetryDelay     = 250 * time.Millisecond
	encoderRetryDelay     = time.Second
	minListenerWriteDelay = 100 * time.Millisecond
	maxListenerWriteDelay = 10 * time.Second
	ringOverwriteSafety   = 500 * time.Millisecond
)

// Verify at compile time that *playlist.Playlist satisfies TrackSource.
var _ playlist.TrackSource = (*playlist.Playlist)(nil)

// TrackInfo holds metadata about the currently playing track.
type TrackInfo struct {
	Title  string
	Artist string
	Album  string
}

// DisconnectHook is called after a listener is removed from the hub.
// It receives the station ID and a snapshot of the disconnected listener.
type DisconnectHook func(stationID string, snap ListenerSnapshot, disconnectedAt time.Time)

// ListenerChangeHook is called when a listener is added to or removed from the hub.
// Delta is 1 for an addition and -1 for a removal.
type ListenerChangeHook func(stationID string, delta int)

// Hub manages the broadcast: it feeds decoded tracks through one continuous
// station encoder, writes MP3 frames to the ring, and manages listeners.
type Hub struct {
	ring   *RingBuffer
	source playlist.TrackSource

	mu                 sync.Mutex
	listeners          map[int64]*Listener
	nextID             int64
	maxListeners       int // 0 = unlimited
	disconnectHook     DisconnectHook
	listenerChangeHook ListenerChangeHook

	stationID     string
	currentTrack  atomic.Value // stores TrackInfo
	listenerCount atomic.Int64
}

// NewHub creates a broadcast hub. maxListeners of 0 means unlimited.
func NewHub(stationID string, source playlist.TrackSource, bufferSizeKB, maxListeners int) *Hub {
	h := &Hub{
		ring:         NewRingBuffer(bufferSizeKB * 1024),
		source:       source,
		listeners:    make(map[int64]*Listener),
		maxListeners: maxListeners,
		stationID:    stationID,
	}
	h.currentTrack.Store(TrackInfo{})
	return h
}

// SetDisconnectHook registers a callback fired after a listener is removed.
func (h *Hub) SetDisconnectHook(hook DisconnectHook) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disconnectHook = hook
}

// SetListenerChangeHook registers a callback fired when a listener is added or removed.
func (h *Hub) SetListenerChangeHook(hook ListenerChangeHook) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listenerChangeHook = hook
}

type preparedTrack struct {
	track library.Track
	src   io.ReadCloser
}

type bufferedReadCloser struct {
	io.Reader
	io.Closer
}

type trackTransition struct {
	outputSample int64
	track        library.Track
}

type transitionQueue struct {
	mu     sync.Mutex
	events []trackTransition
}

func (q *transitionQueue) add(pcmSample int64, track library.Track) {
	outputSample := pcmSample
	if outputSample > 0 {
		outputSample += transcode.EncoderDelaySamples
	}

	q.mu.Lock()
	q.events = append(q.events, trackTransition{outputSample: outputSample, track: track})
	q.mu.Unlock()
}

func (q *transitionQueue) popDue(outputSample int64) (library.Track, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	due := 0
	for due < len(q.events) && q.events[due].outputSample <= outputSample {
		due++
	}
	if due == 0 {
		return library.Track{}, false
	}

	track := q.events[due-1].track
	copy(q.events, q.events[due:])
	clear(q.events[len(q.events)-due:])
	q.events = q.events[:len(q.events)-due]
	return track, true
}

// Run starts the station pipeline. Every source is decoded to a fixed PCM
// profile and fed to one long-lived encoder, eliminating per-track MP3 delay,
// padding, bitrate changes, and sample-rate changes.
func (h *Hub) Run(ctx context.Context) {
	defer h.ring.Close()
	if h.source == nil {
		slog.Error("cannot run station without a track source", "station", h.stationID)
		return
	}

	for ctx.Err() == nil {
		err := h.runGeneration(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("station pipeline stopped unexpectedly")
		}
		slog.Error("station pipeline stopped", "station", h.stationID, "error", err)

		if !waitFor(ctx, encoderRetryDelay) {
			return
		}
	}
}

func (h *Hub) runGeneration(ctx context.Context) error {
	generationCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	encoder, err := transcode.NewEncoder(generationCtx)
	if err != nil {
		return fmt.Errorf("start station encoder: %w", err)
	}

	transitions := &transitionQueue{}
	publishDone := make(chan error, 1)
	go func() {
		publishDone <- h.publishEncoded(generationCtx, encoder, transitions)
		cancel()
	}()

	feedErr := h.feedTracks(generationCtx, cancel, encoder, transitions)
	cancel()
	closeErr := encoder.CloseInput()
	publishErr := <-publishDone
	waitErr := encoder.Wait()

	if ctx.Err() != nil {
		return nil
	}
	if feedErr != nil {
		feedErr = fmt.Errorf("feed station encoder: %w", feedErr)
	}
	if publishErr != nil {
		publishErr = fmt.Errorf("publish station audio: %w", publishErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close station encoder input: %w", closeErr)
	}
	if waitErr != nil {
		waitErr = fmt.Errorf("wait for station encoder: %w", waitErr)
	}
	return errors.Join(feedErr, publishErr, closeErr, waitErr)
}

func (h *Hub) feedTracks(
	ctx context.Context,
	cancel context.CancelFunc,
	encoder io.Writer,
	transitions *transitionQueue,
) error {
	current := h.prepareNext(ctx)
	if current.src == nil {
		return nil
	}

	copyBuffer := make([]byte, 32*1024)
	var totalPCMSamples int64

	for {
		nextCh := make(chan preparedTrack, 1)
		go func() {
			nextCh <- h.prepareNext(ctx)
		}()

		transitions.add(totalPCMSamples, current.track)
		n, copyErr := io.CopyBuffer(
			encoderWriter{Writer: encoder},
			current.src,
			copyBuffer,
		)
		closeErr := current.src.Close()

		if n%transcode.PCMFrameBytes != 0 {
			cancel()
			h.closePrepared(<-nextCh)
			return fmt.Errorf("track %q produced %d unaligned PCM bytes", current.track.Path, n)
		}
		totalPCMSamples += n / transcode.PCMFrameBytes

		var writeErr *encoderWriteError
		if errors.As(copyErr, &writeErr) {
			cancel()
			h.closePrepared(<-nextCh)
			return errors.Join(writeErr, closeErr)
		}

		if ctx.Err() != nil {
			h.closePrepared(<-nextCh)
			return nil
		}
		if err := errors.Join(copyErr, closeErr); err != nil {
			slog.Warn("track decode ended with an error", "path", current.track.Path, "error", err)
		}

		current = <-nextCh
		if current.src == nil {
			return nil
		}
	}
}

type encoderWriter struct {
	io.Writer
}

func (w encoderWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n != len(p) && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil {
		return n, &encoderWriteError{err: err}
	}
	return n, nil
}

type encoderWriteError struct {
	err error
}

func (e *encoderWriteError) Error() string {
	return "write station encoder: " + e.err.Error()
}

func (e *encoderWriteError) Unwrap() error {
	return e.err
}

func (h *Hub) prepareNext(ctx context.Context) preparedTrack {
	for ctx.Err() == nil {
		track := h.source.Next()
		prepared := h.prepareTrack(ctx, track)
		if prepared.src != nil {
			return prepared
		}

		if !waitFor(ctx, decoderRetryDelay) {
			return preparedTrack{}
		}
	}
	return preparedTrack{}
}

func (h *Hub) prepareTrack(ctx context.Context, track library.Track) preparedTrack {
	src, err := transcode.NewPCMReader(ctx, track.Path)
	if err != nil {
		slog.Error("cannot start track decoder", "path", track.Path, "error", err)
		return preparedTrack{}
	}

	buffered := bufio.NewReaderSize(src, pcmPrefetchBytes)
	prefix, readErr := buffered.Peek(pcmPrefetchBytes)
	if len(prefix) == 0 {
		closeErr := src.Close()
		if ctx.Err() == nil {
			slog.Error("track decoder produced no audio", "path", track.Path, "error", errors.Join(readErr, closeErr))
		}
		return preparedTrack{}
	}

	return preparedTrack{
		track: track,
		src: &bufferedReadCloser{
			Reader: buffered,
			Closer: src,
		},
	}
}

func (h *Hub) closePrepared(track preparedTrack) {
	if track.src == nil {
		return
	}
	if err := track.src.Close(); err != nil {
		slog.Debug("cannot close prefetched track", "path", track.track.Path, "error", err)
	}
}

func waitFor(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (h *Hub) publishEncoded(ctx context.Context, encoder io.Reader, transitions *transitionQueue) error {
	reader, err := mp3frame.NewReader(encoder)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("create MP3 frame reader: %w", err)
	}

	epoch := time.Now()
	var totalSamples int64
	throttle := time.NewTimer(0)
	if !throttle.Stop() {
		<-throttle.C
	}
	defer throttle.Stop()

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read encoded MP3 frame: %w", err)
		}
		if frame.Version != 1 || frame.Layer != 3 || frame.Bitrate != transcode.OutputBitrate || frame.SampleRate != transcode.OutputSampleRate {
			return fmt.Errorf(
				"unexpected encoder profile: MPEG-%d layer %d, %d kbps, %d Hz",
				frame.Version,
				frame.Layer,
				frame.Bitrate,
				frame.SampleRate,
			)
		}

		var transition *TrackInfo
		if track, ok := transitions.popDue(totalSamples); ok {
			info := TrackInfo{Title: track.Title, Artist: track.Artist, Album: track.Album}
			h.currentTrack.Store(info)
			transition = &info
			slog.Info("now playing", "title", track.Title, "artist", track.Artist, "path", track.Path)
		}
		h.ring.WriteFrame(frame.Data, transition)

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
				return nil
			}
		}
	}
}

// AddListener registers a new listener and returns it.
// Returns ErrFull if the maximum listener count has been reached.
func (h *Hub) AddListener(wantMeta bool, info ListenerInfo) (*Listener, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.maxListeners > 0 && len(h.listeners) >= h.maxListeners {
		return nil, ErrFull
	}

	id := h.nextID
	h.nextID++

	pos := h.ring.PrerollPos()
	l := NewListener(id, pos, wantMeta, info)
	if h.listenerChangeHook != nil {
		h.listenerChangeHook(h.stationID, 1)
	}
	h.listeners[id] = l
	h.listenerCount.Add(1)

	slog.Info("listener connected", "id", id, "ip", info.IP, "total", h.listenerCount.Load())
	return l, nil
}

// ListenerSnapshot holds a point-in-time view of a listener for status reporting.
type ListenerSnapshot struct {
	ID          int64
	Info        ListenerInfo
	ConnectedAt time.Time
}

// Listeners returns a snapshot of all currently connected listeners.
func (h *Hub) Listeners() []ListenerSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snaps := make([]ListenerSnapshot, 0, len(h.listeners))
	for _, l := range h.listeners {
		snaps = append(snaps, ListenerSnapshot{
			ID:          l.ID,
			Info:        l.Info,
			ConnectedAt: l.ConnectedAt,
		})
	}
	return snaps
}

// RemoveListener unregisters a listener.
func (h *Hub) RemoveListener(l *Listener) {
	l.Close()
	now := time.Now()

	snap := ListenerSnapshot{
		ID:          l.ID,
		Info:        l.Info,
		ConnectedAt: l.ConnectedAt,
	}

	var hook DisconnectHook

	h.mu.Lock()
	if _, ok := h.listeners[l.ID]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.listeners, l.ID)
	h.listenerCount.Add(-1)
	hook = h.disconnectHook
	if h.listenerChangeHook != nil {
		h.listenerChangeHook(h.stationID, -1)
	}
	h.mu.Unlock()

	slog.Info("listener disconnected", "id", l.ID, "total", h.listenerCount.Load())

	if hook != nil {
		hook(h.stationID, snap, now)
	}
}

// ListenerCount returns the current number of connected listeners.
func (h *Hub) ListenerCount() int {
	return int(h.listenerCount.Load())
}

// CurrentTrack returns info about the currently playing track.
func (h *Hub) CurrentTrack() TrackInfo {
	return h.currentTrack.Load().(TrackInfo)
}

// StreamProperties returns the fixed station bitrate and sample rate.
func (h *Hub) StreamProperties() (int, int) {
	return transcode.OutputBitrate, transcode.OutputSampleRate
}

// ListenerWriteTimeout returns a socket deadline shorter than the retention
// remaining after the listener's current lag, capped at ten seconds.
func (h *Hub) ListenerWriteTimeout(pos int64) time.Duration {
	bytesPerSecond := transcode.OutputBitrate * 1000 / 8
	lag := h.ring.WritePos() - pos
	if lag < 0 {
		lag = 0
	}
	remaining := int64(h.ring.size) - lag
	if remaining < 0 {
		remaining = 0
	}
	retention := time.Duration(remaining/int64(bytesPerSecond))*time.Second +
		time.Duration(remaining%int64(bytesPerSecond))*time.Second/time.Duration(bytesPerSecond)
	timeout := retention - ringOverwriteSafety
	if timeout > maxListenerWriteDelay {
		return maxListenerWriteDelay
	}
	if timeout < minListenerWriteDelay {
		return minListenerWriteDelay
	}
	return timeout
}

// Ring returns the hub's ring buffer for listener reads.
func (h *Hub) Ring() *RingBuffer {
	return h.ring
}
