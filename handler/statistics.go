package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"time"

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
	TotalSessions           int64                `json:"total_sessions"`
	TotalListenHours        float64              `json:"total_listen_hours"`
	PeakListeners           int                  `json:"peak_listeners"`
	ActiveListeners         int                  `json:"active_listeners"`
	ActiveListenerCountries []stats.CountryStats `json:"active_listener_countries"`
	TopCountries            []stats.CountryStats `json:"top_countries"`
	TopCities               []stats.CityStats    `json:"top_cities"`
	Daily                   []stats.DailyStats   `json:"daily"`
}

func (s *Statistics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	result, err := s.StatsDB.StationStats(s.Station)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := statsResponse{
		TotalSessions:           result.TotalSessions,
		TotalListenHours:        result.TotalListenHours,
		PeakListeners:           result.PeakListeners,
		ActiveListeners:         s.Hub.ListenerCount(),
		ActiveListenerCountries: activeListenerCountries(s.Hub.Listeners()),
		TopCountries:            result.TopCountries,
		TopCities:               result.TopCities,
		Daily:                   result.Daily,
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
	PeakListeners    int                            `json:"peak_listeners"`
	Stations         map[string]stationStatsPayload `json:"stations"`
}

type stationStatsPayload struct {
	TotalSessions           int64                `json:"total_sessions"`
	TotalListenHours        float64              `json:"total_listen_hours"`
	PeakListeners           int                  `json:"peak_listeners"`
	ActiveListeners         int                  `json:"active_listeners"`
	ActiveListenerCountries []stats.CountryStats `json:"active_listener_countries"`
	TopCountries            []stats.CountryStats `json:"top_countries"`
	TopCities               []stats.CityStats    `json:"top_cities"`
	Daily                   []stats.DailyStats   `json:"daily"`
}

func (g *GlobalStatistics) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	stationIDs := make([]string, 0, len(g.Stations))
	for id := range g.Stations {
		stationIDs = append(stationIDs, id)
	}
	allStats, err := g.StatsDB.AllStats(stationIDs)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	peakListeners, err := g.StatsDB.GlobalPeakListeners()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := globalStatsResponse{
		PeakListeners: peakListeners,
		Stations:      make(map[string]stationStatsPayload, len(g.Stations)),
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
			TotalSessions:           st.TotalSessions,
			TotalListenHours:        st.TotalListenHours,
			PeakListeners:           st.PeakListeners,
			ActiveListeners:         info.Hub.ListenerCount(),
			ActiveListenerCountries: activeListenerCountries(info.Hub.Listeners()),
			TopCountries:            countries,
			TopCities:               cities,
			Daily:                   daily,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// activeListenerCountries aggregates current listener snapshots by country,
// computing session count and listen hours for each.
func activeListenerCountries(snaps []broadcast.ListenerSnapshot) []stats.CountryStats {
	type acc struct {
		country     string
		countryCode string
		sessions    int64
		seconds     float64
	}

	now := time.Now()
	byCode := make(map[string]*acc)

	for _, s := range snaps {
		if s.Info.Country == "" {
			continue
		}
		a, ok := byCode[s.Info.CountryCode]
		if !ok {
			a = &acc{country: s.Info.Country, countryCode: s.Info.CountryCode}
			byCode[s.Info.CountryCode] = a
		}
		a.sessions++
		a.seconds += now.Sub(s.ConnectedAt).Seconds()
	}

	out := make([]stats.CountryStats, 0, len(byCode))
	for _, a := range byCode {
		out = append(out, stats.CountryStats{
			Country:     a.country,
			CountryCode: a.countryCode,
			Sessions:    a.sessions,
			ListenHours: math.Round(a.seconds/3600*10) / 10,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Sessions > out[j].Sessions
	})

	return out
}
