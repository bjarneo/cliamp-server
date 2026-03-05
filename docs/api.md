# API

## Stream Endpoints

Each station exposes three endpoints under its ID prefix:

| Endpoint | Content Type | Description |
|----------|-------------|-------------|
| `/<id>/stream` | `audio/mpeg` | Live MP3 audio stream with ICY metadata |
| `/<id>/stream.pls` | `audio/x-scpls` | PLS playlist file pointing to the stream |
| `/<id>/stream.m3u` | `audio/x-mpegurl` | M3U playlist file pointing to the stream |

The stream endpoint supports ICY metadata. Clients that send the `Icy-MetaData: 1` request header receive inline metadata blocks containing the current track title and artist.

Response headers include `icy-name`, `icy-genre`, `icy-br` (bitrate), `icy-sr` (sample rate), and `icy-metaint` (metadata interval).

## Status Endpoints

| Endpoint | Description |
|----------|-------------|
| `/<id>/status` | JSON status for a single station |
| `/status` | JSON status for all stations |

If `[admin] password` is set, status endpoints require a Bearer token:

```
curl -H "Authorization: Bearer yourpassword" http://localhost:8000/status
```

### Per-Station Status Response

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

### Global Status Response

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

## Statistics Endpoints

Available when `--stats-db` is configured. These are **public** (no password required) and contain no IP addresses.

| Endpoint | Description |
|----------|-------------|
| `/<id>/statistics` | Aggregated listener statistics for a single station |
| `/statistics` | Aggregated listener statistics for all stations |

```
curl http://localhost:8000/radio/statistics
curl http://localhost:8000/statistics
```

### Per-Station Statistics Response

```json
{
  "total_sessions": 8234,
  "total_listen_hours": 2810.3,
  "active_listeners": 42,
  "top_countries": [
    { "country": "Norway", "country_code": "NO", "sessions": 3200, "listen_hours": 1100.2 }
  ],
  "top_cities": [
    { "city": "Oslo", "country_code": "NO", "sessions": 800, "listen_hours": 280.5 }
  ],
  "daily": [
    { "date": "2026-03-05", "sessions": 150, "listen_hours": 48.2 }
  ]
}
```

### Global Statistics Response

```json
{
  "total_sessions": 12847,
  "total_listen_hours": 4231.5,
  "stations": {
    "pop": {
      "total_sessions": 8234,
      "total_listen_hours": 2810.3,
      "active_listeners": 42,
      "top_countries": [],
      "top_cities": [],
      "daily": []
    }
  }
}
```
