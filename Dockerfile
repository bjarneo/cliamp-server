# ── Stage 1: Build static Go binary ──────────────────────────────────
FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /cliamp-server

# ── Stage 2: Extract ffmpeg + its shared libraries ──────────────────
FROM debian:bookworm-slim AS ffmpeg

RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg && \
    rm -rf /var/lib/apt/lists/*

# Copy ffmpeg and only the shared libraries it actually needs
RUN mkdir -p /staging/usr/bin && \
    cp /usr/bin/ffmpeg /staging/usr/bin/ && \
    ldd /usr/bin/ffmpeg | grep -o '/[^ ]*' | while read lib; do \
      dir="/staging$(dirname "$lib")"; \
      mkdir -p "$dir"; \
      cp -L "$lib" "/staging${lib}"; \
    done

# ── Stage 3: Distroless runtime ─────────────────────────────────────
FROM gcr.io/distroless/base-debian12:nonroot

COPY --from=builder /cliamp-server /usr/bin/cliamp-server
COPY --from=ffmpeg /staging/ /

# Config: mount your config.toml at /config/cliamp-server/config.toml
# Music: mount your music directories at paths matching your config
# GeoIP: optionally mount a .mmdb file at a path matching your config
ENV XDG_CONFIG_HOME=/config

EXPOSE 8000

ENTRYPOINT ["/usr/bin/cliamp-server"]
