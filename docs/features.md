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

Listener IP geolocation is powered by MaxMind's GeoLite2-City database. When configured, each connected listener's IP is resolved to a country, city, and coordinates, visible in the status JSON.

To enable it:

1. Download the GeoLite2-City.mmdb file from [MaxMind](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data)
2. Set the path via `--geo-db /path/to/GeoLite2-City.mmdb` or in the config file under `[geo] db_path`

Client IP is extracted from `X-Forwarded-For`, then `X-Real-IP`, then the connection's remote address. This works correctly behind reverse proxies that set these headers.
