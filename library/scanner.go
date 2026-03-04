package library

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"
)

// audioExtensions is the set of file extensions recognized as audio.
var audioExtensions = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
	".opus": true,
	".m4a":  true,
	".aac":  true,
	".webm": true,
	".wma":  true,
}

func Scan(root string, recursive bool) ([]Track, error) {
	var tracks []Track

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("scanner: access error", "path", path, "error", err)
			return nil
		}

		if d.IsDir() && path != root && !recursive {
			return filepath.SkipDir
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if d.IsDir() || !audioExtensions[ext] {
			return nil
		}

		track := Track{Path: path}

		f, err := os.Open(path)
		if err != nil {
			slog.Warn("scanner: cannot open file", "path", path, "error", err)
			track.Title = titleFromFilename(d.Name())
			tracks = append(tracks, track)
			return nil
		}
		defer f.Close()

		m, err := tag.ReadFrom(f)
		if err != nil {
			slog.Debug("scanner: no tags", "path", path, "error", err)
			track.Title = titleFromFilename(d.Name())
			tracks = append(tracks, track)
			return nil
		}

		track.Title = m.Title()
		track.Artist = m.Artist()
		track.Album = m.Album()

		if track.Title == "" {
			track.Title = titleFromFilename(d.Name())
		}

		tracks = append(tracks, track)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].Path < tracks[j].Path
	})

	slog.Info("scanner: complete", "tracks", len(tracks))
	return tracks, nil
}

func titleFromFilename(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}
