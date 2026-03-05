# cliamp-server

An internet radio streaming server written in Go. Shoutcast/ICY compatible, multi-station, live metadata, on-the-fly transcoding, ad injection, GeoIP tracking, and listener statistics.

Point it at a directory of audio files and it starts broadcasting.

Looking for the client? See [cliamp](https://github.com/bjarneo/cliamp).

## Quick Start

```
go build
./cliamp-server --music /path/to/your/mp3s
```

Open `http://localhost:8000/radio/stream` in VLC or any Shoutcast-compatible player.

## Multi-Station

Create `~/.config/cliamp-server/config.toml`:

```toml
[server]
port = 8000

[stations.pop]
name = "Pop Station"
path = "/music/pop"
shuffle = true

[stations.jazz]
name = "Jazz Station"
path = "/music/jazz"
shuffle = true
```

```
./cliamp-server
```

Each station gets its own stream at `/<id>/stream`.

## Docker

```
docker build -t cliamp-server .
docker run -d -v /path/to/music:/music:ro -p 8000:8000 cliamp-server --music /music
```

## Documentation

- [Configuration](docs/configuration.md) — CLI flags, TOML config, station options
- [API](docs/api.md) — Stream endpoints, status endpoints, response format
- [Features](docs/features.md) — Audio format support, ad scheduling, GeoIP, listener statistics
- [Deployment](docs/docker.md) — Docker, nginx reverse proxy

## License

See LICENSE file.
