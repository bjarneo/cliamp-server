package transcode

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// supportedExtensions that need transcoding (everything except .mp3).
var needsTranscodeExt = map[string]bool{
	".wav":  true,
	".flac": true,
	".ogg":  true,
	".opus": true,
	".m4a":  true,
	".aac":  true,
	".webm": true,
	".wma":  true,
}

// NeedsTranscode reports whether the file at path requires ffmpeg transcoding.
// MP3 files are read directly; everything else is piped through ffmpeg.
func NeedsTranscode(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return needsTranscodeExt[ext]
}

// NewReader spawns an ffmpeg process that transcodes the file at path to MP3
// and returns its stdout as an io.ReadCloser. Closing the returned reader
// kills the ffmpeg process and waits for it to exit (no zombies).
func NewReader(ctx context.Context, path string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-i", path,
		"-f", "mp3",
		"-acodec", "libmp3lame",
		"-ab", "128k",
		"-loglevel", "error",
		"pipe:1",
	)
	cmd.Stderr = os.Stderr // drain stderr to avoid blocking ffmpeg

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &transcodeReader{cmd: cmd, pipe: stdout}, nil
}

// transcodeReader wraps the ffmpeg stdout pipe and ensures cleanup on Close.
type transcodeReader struct {
	cmd  *exec.Cmd
	pipe io.ReadCloser
}

func (r *transcodeReader) Read(p []byte) (int, error) {
	return r.pipe.Read(p)
}

func (r *transcodeReader) Close() error {
	r.pipe.Close()

	// Kill the process if still running (context cancellation may have
	// already done this, so ignore errors).
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}

	// Wait to reap the process and avoid zombies.
	r.cmd.Wait()
	return nil
}
