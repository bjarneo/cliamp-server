package handler

import (
	"fmt"
	"net/http"
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
