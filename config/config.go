package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

type StationConfig struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Genre       string `toml:"genre"`
	URL         string `toml:"url"`
	Path        string `toml:"path"`
	Shuffle     bool   `toml:"shuffle"`
	Recursive   bool   `toml:"recursive"`

	// Intro: single MP3 played once at station startup
	IntroFile string `toml:"intro_file"`

	// Ads: MP3 files injected between songs
	AdsPath      string `toml:"ads_path"`
	AdEveryNSongs int    `toml:"ad_every_n_songs"`
	AdEveryNMins  int    `toml:"ad_every_n_minutes"`
	AdShuffle     bool   `toml:"ad_shuffle"`
}

type StreamConfig struct {
	MetaInt      int `toml:"metaint"`
	BufferSize   int `toml:"buffer_size"`
	MaxListeners int `toml:"max_listeners"` // 0 = unlimited
}

type AdminConfig struct {
	Password string `toml:"password"`
}

type LogConfig struct {
	Level string `toml:"level"`
}

type GeoConfig struct {
	DBPath string `toml:"db_path"`
}

type StatsConfig struct {
	DBPath string `toml:"db_path"`
}

type Config struct {
	Server   ServerConfig             `toml:"server"`
	Stream   StreamConfig             `toml:"stream"`
	Admin    AdminConfig              `toml:"admin"`
	Log      LogConfig                `toml:"log"`
	Geo      GeoConfig                `toml:"geo"`
	Stats    StatsConfig              `toml:"stats"`
	Stations map[string]StationConfig `toml:"stations"`
}

func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8000,
		},
		Stream: StreamConfig{
			MetaInt:    8192,
			BufferSize: 512,
		},
		Log: LogConfig{
			Level: "info",
		},
	}
}

// Load reads the config file. If configPath is non-empty, it reads that file
// directly. Otherwise it falls back to ~/.config/cliamp-server/config.toml.
func Load(configPath string) (*Config, error) {
	cfg := Defaults()

	path := configPath
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return cfg, nil
		}
		path = filepath.Join(configDir, "cliamp-server", "config.toml")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && configPath == "" {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if len(c.Stations) == 0 {
		return fmt.Errorf("at least one station is required (use --music or define [stations.*] in config)")
	}

	for id, st := range c.Stations {
		if st.Path == "" {
			return fmt.Errorf("station %q: library path is required", id)
		}

		info, err := os.Stat(st.Path)
		if err != nil {
			return fmt.Errorf("station %q: library path %q: %w", id, st.Path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("station %q: library path %q is not a directory", id, st.Path)
		}

		// Validate intro file
		if st.IntroFile != "" {
			fi, err := os.Stat(st.IntroFile)
			if err != nil {
				return fmt.Errorf("station %q: intro_file %q: %w", id, st.IntroFile, err)
			}
			if fi.IsDir() {
				return fmt.Errorf("station %q: intro_file %q must be a file, not a directory", id, st.IntroFile)
			}
		}

		// Validate ads config
		if st.AdsPath != "" {
			if _, err := os.Stat(st.AdsPath); err != nil {
				return fmt.Errorf("station %q: ads_path %q: %w", id, st.AdsPath, err)
			}
			if st.AdEveryNSongs <= 0 && st.AdEveryNMins <= 0 {
				return fmt.Errorf("station %q: ads_path is set but neither ad_every_n_songs nor ad_every_n_minutes is > 0", id)
			}
		}
	}

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Server.Port)
	}

	if c.Stream.MetaInt < 256 {
		return fmt.Errorf("metaint must be at least 256 bytes")
	}

	if c.Stream.BufferSize < 64 {
		return fmt.Errorf("buffer_size must be at least 64 KB")
	}

	if c.Geo.DBPath != "" {
		if _, err := os.Stat(c.Geo.DBPath); err != nil {
			return fmt.Errorf("geo db_path %q: %w", c.Geo.DBPath, err)
		}
	}

	if c.Stats.DBPath != "" {
		dir := filepath.Dir(c.Stats.DBPath)
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("stats db_path parent directory %q: %w", dir, err)
		}
	}

	return nil
}
