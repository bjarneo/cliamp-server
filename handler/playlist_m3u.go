package handler

import (
	"fmt"
	"net/http"
	"sort"
)

// PlaylistM3U handles GET /{station}/stream.m3u — M3U playlist file.
type PlaylistM3U struct {
	Name   string
	Prefix string
}

func (p *PlaylistM3U) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := streamURL(r, p.Prefix)

	w.Header().Set("Content-Type", "audio/x-mpegurl")
	w.Header().Set("Content-Disposition", "inline; filename=\"stream.m3u\"")

	fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1,%s\n%s\n", p.Name, url)
}

// M3UStation holds the info needed for one entry in the global M3U playlist.
type M3UStation struct {
	Name   string
	Prefix string
}

// GlobalPlaylistM3U handles GET /streams.m3u — M3U file listing all stations.
type GlobalPlaylistM3U struct {
	Stations []M3UStation
}

func (g *GlobalPlaylistM3U) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sorted := make([]M3UStation, len(g.Stations))
	copy(sorted, g.Stations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Prefix < sorted[j].Prefix })

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "audio/x-mpegurl")
	w.Header().Set("Content-Disposition", "inline; filename=\"streams.m3u\"")

	fmt.Fprint(w, "#EXTM3U\n")
	for _, st := range sorted {
		fmt.Fprintf(w, "#EXTINF:-1,%s\n%s\n", st.Name, streamURL(r, st.Prefix))
	}
}
