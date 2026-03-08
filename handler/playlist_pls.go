package handler

import (
	"fmt"
	"net/http"
	"sort"
)

// PlaylistPLS handles GET /{station}/stream.pls — PLS playlist file.
type PlaylistPLS struct {
	Name   string
	Prefix string
}

func (p *PlaylistPLS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := streamURL(r, p.Prefix)

	w.Header().Set("Content-Type", "audio/x-scpls")
	w.Header().Set("Content-Disposition", "inline; filename=\"stream.pls\"")

	fmt.Fprintf(w, "[playlist]\nNumberOfEntries=1\n\nFile1=%s\nTitle1=%s\nLength1=-1\n\nVersion=2\n", url, p.Name)
}

// PLSStation holds the info needed for one entry in the global PLS playlist.
type PLSStation struct {
	Name   string
	Prefix string
}

// GlobalPlaylistPLS handles GET /streams.pls — PLS file listing all stations.
type GlobalPlaylistPLS struct {
	Stations []PLSStation
}

func (g *GlobalPlaylistPLS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Sort by prefix for deterministic output.
	sorted := make([]PLSStation, len(g.Stations))
	copy(sorted, g.Stations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Prefix < sorted[j].Prefix })

	w.Header().Set("Content-Type", "audio/x-scpls")
	w.Header().Set("Content-Disposition", "inline; filename=\"streams.pls\"")

	fmt.Fprintf(w, "[playlist]\nNumberOfEntries=%d\n\n", len(sorted))
	for i, st := range sorted {
		n := i + 1
		fmt.Fprintf(w, "File%d=%s\nTitle%d=%s\nLength%d=-1\n\n", n, streamURL(r, st.Prefix), n, st.Name, n)
	}
	fmt.Fprint(w, "Version=2\n")
}

func streamURL(r *http.Request, prefix string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	return scheme + "://" + r.Host + "/" + prefix + "/stream"
}
