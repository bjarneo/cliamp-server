package playlist

import "cliamp-server/library"

// TrackSource is anything that can supply the next track to play.
// Both *Playlist and scheduler.Scheduler implement this interface.
type TrackSource interface {
	Next() library.Track
}
