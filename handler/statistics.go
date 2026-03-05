package handler

import (
	"encoding/json"
	"net/http"

	"cliamp-server/broadcast"
	"cliamp-server/stats"
)

// Statistics handles GET /{station}/statistics — public per-station statistics.
type Statistics struct {
	Hub     *broadcast.Hub
	StatsDB *stats.DB
	Station string // station ID
}

type statsResponse struct {
	TotalSessions    int64                `json:"total_sessions"`
	TotalListenHours float64              `json:"total_listen_hours"`
	ActiveListeners  int                  `json:"active_listeners"`
	TopCountries     []stats.CountryStats `json:"top_countries"`
	TopCities        []stats.CityStats    `json:"top_cities"`
	Daily            []stats.DailyStats   `json:"daily"`
}

func (s *Statistics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	result, err := s.StatsDB.StationStats(s.Station)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := statsResponse{
		TotalSessions:    result.TotalSessions,
		TotalListenHours: result.TotalListenHours,
		ActiveListeners:  s.Hub.ListenerCount(),
		TopCountries:     result.TopCountries,
		TopCities:        result.TopCities,
		Daily:            result.Daily,
	}

	// Return empty slices instead of null in JSON.
	if resp.TopCountries == nil {
		resp.TopCountries = []stats.CountryStats{}
	}
	if resp.TopCities == nil {
		resp.TopCities = []stats.CityStats{}
	}
	if resp.Daily == nil {
		resp.Daily = []stats.DailyStats{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GlobalStatistics handles GET /statistics — public aggregated statistics.
type GlobalStatistics struct {
	Stations map[string]*StationStatsInfo
	StatsDB  *stats.DB
}

// StationStatsInfo holds runtime info needed for statistics responses.
type StationStatsInfo struct {
	Hub *broadcast.Hub
}

type globalStatsResponse struct {
	TotalSessions    int64                          `json:"total_sessions"`
	TotalListenHours float64                        `json:"total_listen_hours"`
	Stations         map[string]stationStatsPayload `json:"stations"`
}

type stationStatsPayload struct {
	TotalSessions    int64                `json:"total_sessions"`
	TotalListenHours float64              `json:"total_listen_hours"`
	ActiveListeners  int                  `json:"active_listeners"`
	TopCountries     []stats.CountryStats `json:"top_countries"`
	TopCities        []stats.CityStats    `json:"top_cities"`
	Daily            []stats.DailyStats   `json:"daily"`
}

func (g *GlobalStatistics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allStats, err := g.StatsDB.AllStats()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := globalStatsResponse{
		Stations: make(map[string]stationStatsPayload, len(g.Stations)),
	}

	for id, info := range g.Stations {
		st, ok := allStats[id]
		if !ok {
			st = &stats.StationStatsResult{
				TopCountries: []stats.CountryStats{},
				TopCities:    []stats.CityStats{},
				Daily:        []stats.DailyStats{},
			}
		}

		countries := st.TopCountries
		if countries == nil {
			countries = []stats.CountryStats{}
		}
		cities := st.TopCities
		if cities == nil {
			cities = []stats.CityStats{}
		}
		daily := st.Daily
		if daily == nil {
			daily = []stats.DailyStats{}
		}

		resp.TotalSessions += st.TotalSessions
		resp.TotalListenHours += st.TotalListenHours

		resp.Stations[id] = stationStatsPayload{
			TotalSessions:    st.TotalSessions,
			TotalListenHours: st.TotalListenHours,
			ActiveListeners:  info.Hub.ListenerCount(),
			TopCountries:     countries,
			TopCities:        cities,
			Daily:            daily,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
