package handler

import (
	"fmt"
	"net/http"
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
