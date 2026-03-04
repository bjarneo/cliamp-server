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
