package library

// Track represents a single MP3 file in the library.
type Track struct {
	Path     string // Absolute path to the MP3 file
	Title    string // From ID3 tag or filename
	Artist   string // From ID3 tag
	Album    string // From ID3 tag
	Duration float64 // Duration in seconds (0 if unknown)
	Bitrate  int    // Bitrate in kbps (0 if unknown)
}

// StreamTitle returns the ICY-compatible "Artist - Title" string.
func (t *Track) StreamTitle() string {
	if t.Artist != "" && t.Title != "" {
		return t.Artist + " - " + t.Title
	}
	if t.Title != "" {
		return t.Title
	}
	return t.Path
}
