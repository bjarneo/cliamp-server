package config

import (
	"fmt"
	"os"
	"strconv"
)

const Version = "0.1.0"

func usage() {
	fmt.Fprintf(os.Stderr, `cliamp-server v%s — Internet Radio Streaming Server

Usage:
  cliamp-server [flags]

Quick start (single station at /radio/stream):
  cliamp-server --music /path/to/mp3s

For multiple stations, use a config file with [stations.*] sections.

Flags:
  --music <path>            Path to MP3 directory (creates station "radio")
  --port <port>             Listen port (default: 8000)
  --shuffle                 Enable shuffle mode
  --no-shuffle              Disable shuffle mode
  --name <name>             Station name
  --intro <path>            Intro MP3 file (played once at startup)
  --ads <path>              Ads directory or single MP3 file
  --ad-every-songs <n>      Play ad after every N songs (default: 0 = off)
  --ad-every-minutes <n>    Play ad after every N minutes (default: 0 = off)
  --max-listeners <n>       Max concurrent listeners per station (0 = unlimited)
  --password <token>        Admin password for /status endpoints
  --geo-db <path>           Path to MaxMind GeoLite2-City.mmdb file
  --log-level <level>       Log level (debug, info, warn, error)
  -h, --help                Show this help
  -v, --version             Show version
`, Version)
}

func ParseFlags(cfg *Config) (exit bool) {
	args := os.Args[1:]

	var musicPath, stationName string
	var introPath, adsPath string
	var adEverySongs, adEveryMins int
	shuffleSet := false
	shuffle := true

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			usage()
			return true
		case "-v", "--version":
			fmt.Printf("cliamp-server v%s\n", Version)
			return true
		case "--music":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --music requires a path")
				os.Exit(1)
			}
			musicPath = args[i]
		case "--port":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --port requires a number")
				os.Exit(1)
			}
			port, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid port: %s\n", args[i])
				os.Exit(1)
			}
			cfg.Server.Port = port
		case "--shuffle":
			shuffleSet = true
			shuffle = true
		case "--no-shuffle":
			shuffleSet = true
			shuffle = false
		case "--name":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --name requires a value")
				os.Exit(1)
			}
			stationName = args[i]
		case "--intro":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --intro requires a path")
				os.Exit(1)
			}
			introPath = args[i]
		case "--ads":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --ads requires a path")
				os.Exit(1)
			}
			adsPath = args[i]
		case "--ad-every-songs":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --ad-every-songs requires a number")
				os.Exit(1)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid ad-every-songs: %s\n", args[i])
				os.Exit(1)
			}
			adEverySongs = n
		case "--ad-every-minutes":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --ad-every-minutes requires a number")
				os.Exit(1)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid ad-every-minutes: %s\n", args[i])
				os.Exit(1)
			}
			adEveryMins = n
		case "--max-listeners":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --max-listeners requires a number")
				os.Exit(1)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid max-listeners: %s\n", args[i])
				os.Exit(1)
			}
			cfg.Stream.MaxListeners = n
		case "--password":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --password requires a value")
				os.Exit(1)
			}
			cfg.Admin.Password = args[i]
		case "--geo-db":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --geo-db requires a path")
				os.Exit(1)
			}
			cfg.Geo.DBPath = args[i]
		case "--log-level":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --log-level requires a value")
				os.Exit(1)
			}
			cfg.Log.Level = args[i]
		default:
			fmt.Fprintf(os.Stderr, "error: unknown flag: %s\n", args[i])
			usage()
			os.Exit(1)
		}
	}

	// If --music is provided, create/update the "radio" station
	if musicPath != "" {
		if cfg.Stations == nil {
			cfg.Stations = make(map[string]StationConfig)
		}
		st := cfg.Stations["radio"]
		st.Path = musicPath
		st.Shuffle = true
		st.Recursive = true
		if st.Name == "" {
			st.Name = "cliamp radio"
		}
		if st.Description == "" {
			st.Description = "24/7 Music"
		}
		if st.Genre == "" {
			st.Genre = "Various"
		}
		cfg.Stations["radio"] = st
	}

	// Apply intro/ads flags to the "radio" station
	if cfg.Stations != nil {
		if st, ok := cfg.Stations["radio"]; ok {
			if introPath != "" {
				st.IntroFile = introPath
			}
			if adsPath != "" {
				st.AdsPath = adsPath
			}
			if adEverySongs > 0 {
				st.AdEveryNSongs = adEverySongs
			}
			if adEveryMins > 0 {
				st.AdEveryNMins = adEveryMins
			}
			cfg.Stations["radio"] = st
		}
	}

	// Apply --name and --shuffle overrides to the "radio" station
	if stationName != "" {
		if cfg.Stations != nil {
			if st, ok := cfg.Stations["radio"]; ok {
				st.Name = stationName
				cfg.Stations["radio"] = st
			}
		}
	}
	if shuffleSet {
		if cfg.Stations != nil {
			if st, ok := cfg.Stations["radio"]; ok {
				st.Shuffle = shuffle
				cfg.Stations["radio"] = st
			}
		}
	}

	return false
}
