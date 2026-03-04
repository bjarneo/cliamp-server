# Docker

A multi-stage distroless Dockerfile is included. The final image runs as a nonroot user with no shell and no package manager.

## Build

```
docker build -t cliamp-server .
```

## Run with a Config File

```
docker run -d \
  -v /path/to/config.toml:/config/cliamp-server/config.toml:ro \
  -v /path/to/music:/music:ro \
  -p 8000:8000 \
  cliamp-server
```

The container sets `XDG_CONFIG_HOME=/config`, so the server reads `/config/cliamp-server/config.toml` automatically. Mount your config file to that path.

Music directories, GeoIP databases, intro files, and ad directories must also be mounted as volumes at paths matching your config.

## Run with CLI Flags

```
docker run -d \
  -v /path/to/music:/music:ro \
  -p 8000:8000 \
  cliamp-server --music /music --shuffle
```

## Volume Reference

| Container Path | Purpose | Required |
|----------------|---------|----------|
| `/config/cliamp-server/config.toml` | TOML config file | Only without `--music` |
| `/music/...` | Audio file directories | Yes |
| GeoIP `.mmdb` path | MaxMind database | No |
| Ads directory path | Ad MP3 files | No |
| Intro file path | Station intro MP3 | No |
