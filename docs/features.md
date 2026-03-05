# Features

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

Listener IP geolocation uses a `.mmdb` database. When configured, each connected listener's IP is resolved to a country, city, and coordinates, visible in the status JSON.

To enable it:

1. Download the [DB-IP City Lite](https://download.db-ip.com/free/dbip-city-lite-2026-03.mmdb.gz) database (free, no signup)
2. Unpack it: `gunzip dbip-city-lite-2026-03.mmdb.gz`
3. Set the path via `--geo-db /path/to/dbip-city-lite-2026-03.mmdb` or in the config file under `[geo] db_path`

Client IP is extracted from `X-Forwarded-For`, then `X-Real-IP`, then the connection's remote address. This works correctly behind reverse proxies that set these headers.

## Listener Statistics

When a SQLite database path is configured, the server records each listener session on disconnect. No IP addresses are stored — only geo information (country, city, coordinates), connection times, and duration.

To enable it:

1. Pass `--stats-db /path/to/stats.db` or set `[stats] db_path` in the config file
2. The database file is created automatically; the parent directory must exist

The public `/statistics` and `/<id>/statistics` endpoints return aggregated data: total sessions, listen hours, top 10 countries, top 10 cities, and a daily breakdown for the last 30 days. These endpoints require no authentication.

Statistics work best when combined with GeoIP — without it, country and city fields will be empty.
