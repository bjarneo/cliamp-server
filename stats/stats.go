package stats

import (
	"database/sql"
	"math"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const statsCacheTTL = time.Minute

// DB wraps a SQLite database for persisting listener session statistics.
type DB struct {
	db *sql.DB

	cacheMu    sync.Mutex
	cache      map[string]cachedStats
	generation map[string]uint64
	inflight   map[string]chan struct{}
}

type cachedStats struct {
	result    *StationStatsResult
	expiresAt time.Time
}

// Session holds the data recorded when a listener disconnects.
type Session struct {
	Station         string
	Country         string
	CountryCode     string
	City            string
	Latitude        float64
	Longitude       float64
	ConnectedAt     time.Time
	DisconnectedAt  time.Time
	DurationSeconds int64
}

// Open opens (or creates) a SQLite database at path and initialises the schema.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// WAL mode allows concurrent reads while writing.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	const schema = `CREATE TABLE IF NOT EXISTS listener_sessions (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		station          TEXT    NOT NULL,
		country          TEXT    NOT NULL DEFAULT '',
		country_code     TEXT    NOT NULL DEFAULT '',
		city             TEXT    NOT NULL DEFAULT '',
		latitude         REAL    NOT NULL DEFAULT 0,
		longitude        REAL    NOT NULL DEFAULT 0,
		connected_at     TEXT    NOT NULL,
		disconnected_at  TEXT    NOT NULL,
		duration_seconds INTEGER NOT NULL
	)`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	const peakSchema = `CREATE TABLE IF NOT EXISTS listener_peaks (
		scope          TEXT    PRIMARY KEY,
		peak_listeners INTEGER NOT NULL
	)`
	if _, err := db.Exec(peakSchema); err != nil {
		db.Close()
		return nil, err
	}
	const trackPlaySchema = `CREATE TABLE IF NOT EXISTS track_plays (
		station TEXT    NOT NULL,
		track_id TEXT   NOT NULL,
		plays    INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (station, track_id)
	)`
	if _, err := db.Exec(trackPlaySchema); err != nil {
		db.Close()
		return nil, err
	}
	if err := backfillListenerPeaks(db); err != nil {
		db.Close()
		return nil, err
	}

	// These indexes cover the per-station aggregates used by the public
	// statistics endpoints. Without them, every request scans the full history.
	const indexes = `
		CREATE INDEX IF NOT EXISTS idx_listener_sessions_station_connected_at
		ON listener_sessions (station, connected_at, duration_seconds);

		CREATE INDEX IF NOT EXISTS idx_listener_sessions_station_country
		ON listener_sessions (station, country, country_code, duration_seconds)
		WHERE country != '';

		CREATE INDEX IF NOT EXISTS idx_listener_sessions_station_city
		ON listener_sessions (station, city, country_code, duration_seconds)
		WHERE city != '';`
	if _, err := db.Exec(indexes); err != nil {
		db.Close()
		return nil, err
	}

	return &DB{
		db:         db,
		cache:      make(map[string]cachedStats),
		generation: make(map[string]uint64),
		inflight:   make(map[string]chan struct{}),
	}, nil
}

// Record inserts a completed listener session.
func (d *DB) Record(s Session) error {
	_, err := d.db.Exec(
		`INSERT INTO listener_sessions
			(station, country, country_code, city, latitude, longitude,
			 connected_at, disconnected_at, duration_seconds)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Station,
		s.Country,
		s.CountryCode,
		s.City,
		s.Latitude,
		s.Longitude,
		s.ConnectedAt.UTC().Format(time.RFC3339),
		s.DisconnectedAt.UTC().Format(time.RFC3339),
		s.DurationSeconds,
	)
	if err != nil {
		return err
	}

	d.cacheMu.Lock()
	d.generation[s.Station]++
	delete(d.cache, s.Station)
	d.cacheMu.Unlock()
	return nil
}

// RecordTrackPlay increments the play count for one station track.
func (d *DB) RecordTrackPlay(station, trackID string) error {
	_, err := d.db.Exec(
		`INSERT INTO track_plays (station, track_id, plays) VALUES (?, ?, 1)
		 ON CONFLICT(station, track_id) DO UPDATE SET plays = plays + 1`,
		station,
		trackID,
	)
	return err
}

// TrackPlayCounts returns all recorded track play counts for a station.
func (d *DB) TrackPlayCounts(station string) (map[string]int64, error) {
	rows, err := d.db.Query(
		`SELECT track_id, plays FROM track_plays WHERE station = ?`,
		station,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var trackID string
		var plays int64
		if err := rows.Scan(&trackID, &plays); err != nil {
			return nil, err
		}
		counts[trackID] = plays
	}
	return counts, rows.Err()
}

// StationStatsResult holds aggregated statistics for a single station.
type StationStatsResult struct {
	TotalSessions    int64          `json:"total_sessions"`
	TotalListenHours float64        `json:"total_listen_hours"`
	PeakListeners    int            `json:"peak_listeners"`
	TopCountries     []CountryStats `json:"top_countries"`
	TopCities        []CityStats    `json:"top_cities"`
	Daily            []DailyStats   `json:"daily"`
}

// CountryStats holds aggregated data for a country.
type CountryStats struct {
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Sessions    int64   `json:"sessions"`
	ListenHours float64 `json:"listen_hours"`
}

// CityStats holds aggregated data for a city.
type CityStats struct {
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	Sessions    int64   `json:"sessions"`
	ListenHours float64 `json:"listen_hours"`
}

// DailyStats holds aggregated data for a single day.
type DailyStats struct {
	Date        string  `json:"date"`
	Sessions    int64   `json:"sessions"`
	ListenHours float64 `json:"listen_hours"`
}

// StationStats returns aggregated statistics for a single station.
func (d *DB) StationStats(station string) (*StationStatsResult, error) {
	for {
		d.cacheMu.Lock()
		if cached, ok := d.cache[station]; ok && time.Now().Before(cached.expiresAt) {
			result := cloneStats(cached.result)
			d.cacheMu.Unlock()
			return result, nil
		}
		if done, ok := d.inflight[station]; ok {
			d.cacheMu.Unlock()
			<-done
			continue
		}
		generation := d.generation[station]
		done := make(chan struct{})
		d.inflight[station] = done
		d.cacheMu.Unlock()

		result, err := d.stationStats(station)

		d.cacheMu.Lock()
		delete(d.inflight, station)
		if err == nil && d.generation[station] == generation {
			d.cache[station] = cachedStats{
				result:    cloneStats(result),
				expiresAt: time.Now().Add(statsCacheTTL),
			}
		}
		close(done)
		d.cacheMu.Unlock()
		return result, err
	}
}

func (d *DB) stationStats(station string) (*StationStatsResult, error) {
	result := &StationStatsResult{}

	// Totals
	err := d.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(duration_seconds), 0)
		 FROM listener_sessions WHERE station = ?`, station,
	).Scan(&result.TotalSessions, &result.TotalListenHours)
	if err != nil {
		return nil, err
	}
	result.TotalListenHours = roundHours(result.TotalListenHours)
	result.PeakListeners, err = d.StationPeakListeners(station)
	if err != nil {
		return nil, err
	}

	// Top 10 countries
	result.TopCountries, err = d.topCountries(station)
	if err != nil {
		return nil, err
	}

	// Top 10 cities
	result.TopCities, err = d.topCities(station)
	if err != nil {
		return nil, err
	}

	// Daily breakdown (last 30 days)
	result.Daily, err = d.daily(station)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func cloneStats(in *StationStatsResult) *StationStatsResult {
	if in == nil {
		return nil
	}
	return &StationStatsResult{
		TotalSessions:    in.TotalSessions,
		TotalListenHours: in.TotalListenHours,
		PeakListeners:    in.PeakListeners,
		TopCountries:     append([]CountryStats(nil), in.TopCountries...),
		TopCities:        append([]CityStats(nil), in.TopCities...),
		Daily:            append([]DailyStats(nil), in.Daily...),
	}
}

// AllStats returns aggregated statistics for stationIDs, keyed by station ID.
func (d *DB) AllStats(stationIDs []string) (map[string]*StationStatsResult, error) {
	result := make(map[string]*StationStatsResult, len(stationIDs))
	for _, id := range stationIDs {
		s, err := d.StationStats(id)
		if err != nil {
			return nil, err
		}
		result[id] = s
	}
	return result, nil
}

func (d *DB) topCountries(station string) ([]CountryStats, error) {
	rows, err := d.db.Query(
		`SELECT country, country_code,
		        COUNT(*) AS sessions,
		        SUM(duration_seconds) AS secs
		 FROM listener_sessions
		 WHERE station = ? AND country != ''
		 GROUP BY country, country_code
		 ORDER BY sessions DESC
		 LIMIT 10`, station,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CountryStats
	for rows.Next() {
		var c CountryStats
		var secs float64
		if err := rows.Scan(&c.Country, &c.CountryCode, &c.Sessions, &secs); err != nil {
			return nil, err
		}
		c.ListenHours = roundHours(secs)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) topCities(station string) ([]CityStats, error) {
	rows, err := d.db.Query(
		`SELECT city, country_code,
		        COUNT(*) AS sessions,
		        SUM(duration_seconds) AS secs
		 FROM listener_sessions
		 WHERE station = ? AND city != ''
		 GROUP BY city, country_code
		 ORDER BY sessions DESC
		 LIMIT 10`, station,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CityStats
	for rows.Next() {
		var c CityStats
		var secs float64
		if err := rows.Scan(&c.City, &c.CountryCode, &c.Sessions, &secs); err != nil {
			return nil, err
		}
		c.ListenHours = roundHours(secs)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) daily(station string) ([]DailyStats, error) {
	rows, err := d.db.Query(
		`SELECT DATE(connected_at) AS day,
		        COUNT(*) AS sessions,
		        SUM(duration_seconds) AS secs
		 FROM listener_sessions
		 WHERE station = ?
		   AND connected_at >= DATE('now', '-30 days')
		 GROUP BY day
		 ORDER BY day`, station,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DailyStats
	for rows.Next() {
		var d DailyStats
		var secs float64
		if err := rows.Scan(&d.Date, &d.Sessions, &secs); err != nil {
			return nil, err
		}
		d.ListenHours = roundHours(secs)
		out = append(out, d)
	}
	return out, rows.Err()
}

// Close closes the database.
func (d *DB) Close() error {
	return d.db.Close()
}

// roundHours converts seconds to hours rounded to 1 decimal place.
func roundHours(seconds float64) float64 {
	return math.Round(seconds/3600*10) / 10
}
