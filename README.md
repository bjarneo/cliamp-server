# cliamp-server

An internet radio streaming server written in Go. It serves Shoutcast/ICY compatible audio streams over HTTP, supports multiple simultaneous stations, live metadata, on the fly transcoding, ad injection, and optional GeoIP listener tracking.

Point it at a directory of audio files and it starts broadcasting.

## Quick Start

```
go build
./cliamp-server --music /path/to/your/mp3s
```

This creates a single station called "radio" on port 8000. Open `http://localhost:8000/radio/stream` in VLC, foobar2000, or any player that supports HTTP streams.

## Installation

Requires Go 1.25 or later.

```
git clone <repo-url>
cd cliamp-server
go build
```

The build produces a single binary with no runtime dependencies other than `ffmpeg` (only needed if you serve non-MP3 audio files).

## Usage

There are two ways to run the server: with CLI flags for a quick single station, or with a TOML config file for full multi-station setups.

### Single Station (CLI Flags)

```
./cliamp-server --music /path/to/mp3s --shuffle --port 8000
```

All available flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--music <path>` | Path to audio directory (creates station "radio") | |
| `--port <port>` | Listen port | 8000 |
| `--shuffle` | Enable shuffle mode | on |
| `--no-shuffle` | Disable shuffle mode | |
| `--name <name>` | Station name | "cliamp radio" |
| `--intro <path>` | Intro MP3 played once when a listener connects | |
| `--ads <path>` | Ads directory or single MP3 file | |
| `--ad-every-songs <n>` | Insert an ad after every N songs | 0 (off) |
| `--ad-every-minutes <n>` | Insert an ad after every N minutes | 0 (off) |
| `--geo-db <path>` | Path to MaxMind GeoLite2-City.mmdb file | |
| `--log-level <level>` | Log level: debug, info, warn, error | info |

### Multi-Station (Config File)

Create a config file at `~/.config/cliamp-server/config.toml`:

```toml
[server]
host = "0.0.0.0"
port = 8000

[stream]
metaint = 8192       # ICY metadata interval in bytes
buffer_size = 512    # Ring buffer size in KB (512 ~ 32s of 128kbps audio)

[admin]
password = ""        # Bearer token for /status endpoints (empty = open)

[log]
level = "info"

[geo]
db_path = ""         # Path to MaxMind GeoLite2-City.mmdb (optional)

[stations.pop]
name = "Pop Station"
description = "Pop Music 24/7"
genre = "Pop"
url = ""
path = "/music/pop"
shuffle = true
recursive = true

[stations.jazz]
name = "Jazz Station"
description = "Smooth Jazz"
genre = "Jazz"
url = ""
path = "/music/jazz"
shuffle = true
recursive = true
```

Each `[stations.<id>]` block creates a station. The `<id>` determines the URL prefix: `[stations.pop]` becomes `/pop/stream`.

Run the server with no flags and it reads the config automatically:

```
./cliamp-server
```

CLI flags override config file values when both are present. The config file is optional; defaults are used for any missing field.

### Station Options

Every station supports these fields:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Display name shown in ICY metadata |
| `description` | string | Station description |
| `genre` | string | Genre tag sent in ICY headers |
| `url` | string | Station URL sent in ICY headers |
| `path` | string | **Required.** Directory containing audio files |
| `shuffle` | bool | Randomize playback order |
| `recursive` | bool | Scan subdirectories for audio files |
| `intro_file` | string | MP3 played once per listener before joining the live broadcast |
| `ads_path` | string | Directory of ad MP3 files (or a single file) |
| `ad_every_n_songs` | int | Play an ad after every N music tracks |
| `ad_every_n_minutes` | int | Play an ad after every N minutes |
| `ad_shuffle` | bool | Randomize ad selection order |

## API

### Stream Endpoints

Each station exposes three endpoints under its ID prefix:

| Endpoint | Content Type | Description |
|----------|-------------|-------------|
| `/<id>/stream` | `audio/mpeg` | Live MP3 audio stream with ICY metadata |
| `/<id>/stream.pls` | `audio/x-scpls` | PLS playlist file pointing to the stream |
| `/<id>/stream.m3u` | `audio/x-mpegurl` | M3U playlist file pointing to the stream |

The stream endpoint supports ICY metadata. Clients that send the `Icy-MetaData: 1` request header receive inline metadata blocks containing the current track title and artist.

Response headers include `icy-name`, `icy-genre`, `icy-br` (bitrate), `icy-sr` (sample rate), and `icy-metaint` (metadata interval).

### Status Endpoints

| Endpoint | Description |
|----------|-------------|
| `/<id>/status` | JSON status for a single station |
| `/status` | JSON status for all stations |

If `[admin] password` is set, status endpoints require a Bearer token:

```
curl -H "Authorization: Bearer yourpassword" http://localhost:8000/status
```

#### Per-Station Status Response

```json
{
  "station": "Pop Station",
  "listeners": 12,
  "listener_details": [
    {
      "ip": "203.0.113.42",
      "country": "Norway",
      "country_code": "NO",
      "city": "Oslo",
      "latitude": 59.9139,
      "longitude": 10.7522,
      "connected_at": "2026-03-04T10:30:00Z",
      "duration_seconds": 300
    }
  ],
  "current_track": {
    "title": "Song Title",
    "artist": "Artist Name",
    "album": "Album Name"
  },
  "uptime": "5h30m45s",
  "uptime_seconds": 19845,
  "playlist_length": 247
}
```

#### Global Status Response

```json
{
  "stations": {
    "pop": {
      "name": "Pop Station",
      "listeners": 12,
      "listener_details": [],
      "current_track": { "title": "", "artist": "", "album": "" },
      "playlist_length": 247
    },
    "jazz": {
      "name": "Jazz Station",
      "listeners": 3,
      "listener_details": [],
      "current_track": { "title": "", "artist": "", "album": "" },
      "playlist_length": 89
    }
  },
  "total_listeners": 15,
  "uptime": "5h30m45s",
  "uptime_seconds": 19845
}
```

## Audio Format Support

MP3 files are streamed directly without processing. All other supported formats are transcoded to 128 kbps MP3 on the fly using ffmpeg:

| Format | Extension | Transcoded |
|--------|-----------|------------|
| MP3 | `.mp3` | No (native) |
| WAV | `.wav` | Yes |
| FLAC | `.flac` | Yes |
| OGG Vorbis | `.ogg` | Yes |
| Opus | `.opus` | Yes |
| AAC/M4A | `.m4a`, `.aac` | Yes |
| WebM | `.webm` | Yes |
| WMA | `.wma` | Yes |

Transcoding requires `ffmpeg` with `libmp3lame` to be installed and available on `PATH`.

Track metadata (title, artist, album) is read from ID3 tags. Files without tags fall back to the filename as the title.

## Ad Scheduling

Ads are injected between music tracks based on two independent triggers. Either one firing will cause the next track to be an ad:

1. **Song count**: After every N music tracks (`ad_every_n_songs`)
2. **Time interval**: After every N minutes (`ad_every_n_minutes`)

Set `ads_path` to a directory of MP3 files or a single MP3 file. When `ad_shuffle` is true, ads are played in random order. When false, they rotate sequentially.

Both triggers reset after each ad plays.

## GeoIP

Listener IP geolocation is powered by MaxMind's GeoLite2-City database. When configured, each connected listener's IP is resolved to a country, city, and coordinates, visible in the status JSON.

To enable it:

1. Download the GeoLite2-City.mmdb file from [MaxMind](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data)
2. Set the path via `--geo-db /path/to/GeoLite2-City.mmdb` or in the config file under `[geo] db_path`

Client IP is extracted from `X-Forwarded-For`, then `X-Real-IP`, then the connection's remote address. This works correctly behind reverse proxies that set these headers.

## Docker

A multi-stage distroless Dockerfile is included. The final image runs as a nonroot user with no shell and no package manager.

### Build

```
docker build -t cliamp-server .
```

### Run with a Config File

```
docker run -d \
  -v /path/to/config.toml:/config/cliamp-server/config.toml:ro \
  -v /path/to/music:/music:ro \
  -p 8000:8000 \
  cliamp-server
```

The container sets `XDG_CONFIG_HOME=/config`, so the server reads `/config/cliamp-server/config.toml` automatically. Mount your config file to that path.

Music directories, GeoIP databases, intro files, and ad directories must also be mounted as volumes at paths matching your config.

### Run with CLI Flags

```
docker run -d \
  -v /path/to/music:/music:ro \
  -p 8000:8000 \
  cliamp-server --music /music --shuffle
```

### Volume Reference

| Container Path | Purpose | Required |
|----------------|---------|----------|
| `/config/cliamp-server/config.toml` | TOML config file | Only without `--music` |
| `/music/...` | Audio file directories | Yes |
| GeoIP `.mmdb` path | MaxMind database | No |
| Ads directory path | Ad MP3 files | No |
| Intro file path | Station intro MP3 | No |

## Architecture

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

### How Streaming Works

The broadcast hub reads audio files frame by frame, throttled to real time using each frame's sample count and the stream's sample rate. Frames are written into a ring buffer sized to hold roughly 32 seconds of audio at 128 kbps.

When a listener connects, it is assigned a read position a few frames behind the write head. This gives the MP3 decoder enough prior frames to fill the Layer III bit reservoir before producing audio. The listener then reads from the ring buffer, receiving the same frames as every other listener, synchronized to real time.

If a listener falls too far behind (its read position gets overwritten by the write head wrapping around), it is disconnected.

ICY metadata is interleaved into the stream at a fixed byte interval (default 8192 bytes). Clients that request metadata via the `Icy-MetaData: 1` header receive title updates inline as tracks change.

Non-MP3 files are transcoded on the fly by spawning an ffmpeg subprocess that pipes 128 kbps MP3 to stdout. The next track is prefetched during playback so transitions are gapless.

### Graceful Shutdown

The server listens for `SIGINT` and `SIGTERM`. On receiving either signal, it stops accepting new connections, finishes in-flight responses, and exits cleanly.

## License

See LICENSE file.
