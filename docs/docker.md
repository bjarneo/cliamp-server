# Deployment

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

## Nginx Reverse Proxy

Audio streams are long-lived connections, so buffering and timeouts must be adjusted.

```nginx
upstream cliamp {
    server 127.0.0.1:8000;
}

server {
    listen 80;
    server_name radio.example.com;

    location / {
        proxy_pass http://cliamp;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

        # streaming: disable buffering and size limits
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_max_temp_file_size 0;

        # no timeout on long-lived stream connections
        proxy_read_timeout 24h;
        proxy_send_timeout 24h;
    }
}
```

Replace `radio.example.com` with your domain. Add TLS with certbot or your own certificates.

The `X-Real-IP` and `X-Forwarded-For` headers are used by cliamp-server for GeoIP listener tracking.
