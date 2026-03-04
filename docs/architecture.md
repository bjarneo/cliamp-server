# Architecture

## Project Structure

```
main.go                 Entry point: loads config, builds stations, starts server

config/
  config.go             TOML config loading and validation
  flags.go              CLI flag parsing

server/
  server.go             HTTP server and route registration

handler/
  stream.go             Live audio stream handler with ICY metadata
  status.go             Per-station and global JSON status
  playlist_pls.go       PLS playlist generation
  playlist_m3u.go       M3U playlist generation

broadcast/
  hub.go                Core broadcast loop: reads frames, writes to ring buffer
  ringbuf.go            Lock-free ring buffer with absolute positioning
  listener.go           Per-listener state and connection metadata

library/
  scanner.go            Directory scanner for audio files with ID3 tag reading
  track.go              Track metadata struct

playlist/
  playlist.go           Ordered/shuffled track rotation
  source.go             TrackSource interface (implemented by playlist and scheduler)

scheduler/
  scheduler.go          Ad injection logic with song count and time triggers
  adpool.go             Ad file pool with sequential or shuffled rotation

transcode/
  transcode.go          On-the-fly audio transcoding via ffmpeg subprocess

mp3frame/
  frame.go              MP3 frame header parsing (sync word, bitrate, sample rate)
  reader.go             Frame-aligned MP3 reader with ID3v2 skipping

icy/
  metadata.go           ICY metadata block encoding
  writer.go             Interleaves audio bytes with ICY metadata at fixed intervals

geo/
  geo.go                MaxMind GeoIP2 database wrapper
```

## How Streaming Works

The broadcast hub reads audio files frame by frame, throttled to real time using each frame's sample count and the stream's sample rate. Frames are written into a ring buffer sized to hold roughly 32 seconds of audio at 128 kbps.

When a listener connects, it is assigned a read position a few frames behind the write head. This gives the MP3 decoder enough prior frames to fill the Layer III bit reservoir before producing audio. The listener then reads from the ring buffer, receiving the same frames as every other listener, synchronized to real time.

If a listener falls too far behind (its read position gets overwritten by the write head wrapping around), it is disconnected.

ICY metadata is interleaved into the stream at a fixed byte interval (default 8192 bytes). Clients that request metadata via the `Icy-MetaData: 1` header receive title updates inline as tracks change.

Non-MP3 files are transcoded on the fly by spawning an ffmpeg subprocess that pipes 128 kbps MP3 to stdout. The next track is prefetched during playback so transitions are gapless.

## Graceful Shutdown

The server listens for `SIGINT` and `SIGTERM`. On receiving either signal, it stops accepting new connections, finishes in-flight responses, and exits cleanly.
