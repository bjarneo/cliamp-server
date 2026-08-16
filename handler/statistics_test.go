package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cliamp-server/broadcast"
	"cliamp-server/stats"
)

func TestStatisticsResponsesIncludePeakListeners(t *testing.T) {
	db, err := stats.Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tracker, err := stats.NewPeakTracker(db, []string{"lofi", "jazz"})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		station string
		delta   int
	}{
		{"lofi", 1},
		{"jazz", 1},
		{"lofi", -1},
		{"jazz", -1},
	} {
		if err := tracker.Change(change.station, change.delta); err != nil {
			t.Fatal(err)
		}
	}

	lofiHub := broadcast.NewHub("lofi", nil, 64, 0)
	jazzHub := broadcast.NewHub("jazz", nil, 64, 0)

	t.Run("station", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/lofi/statistics", nil)
		(&Statistics{Hub: lofiHub, StatsDB: db, Station: "lofi"}).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var response statsResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.PeakListeners != 1 {
			t.Errorf("peak listeners = %d, want 1", response.PeakListeners)
		}
	})

	t.Run("global", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/statistics", nil)
		(&GlobalStatistics{
			StatsDB: db,
			Stations: map[string]*StationStatsInfo{
				"lofi": {Hub: lofiHub},
				"jazz": {Hub: jazzHub},
			},
		}).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var response globalStatsResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.PeakListeners != 2 {
			t.Errorf("peak listeners = %d, want 2", response.PeakListeners)
		}
		if response.Stations["lofi"].PeakListeners != 1 {
			t.Errorf("lofi peak listeners = %d, want 1", response.Stations["lofi"].PeakListeners)
		}
	})
}
