// Package settings remembers the user's choices in a JSON file. It holds
// nothing per-download or sensitive: no URL, cookies, range or browser profile.
package settings

import (
	"time"

	"github.com/aatng-gh/ytgrab/internal/request"
)

type Settings struct {
	Options   request.Options `json:"options"`
	Browser   request.Browser `json:"browser"`
	OutputDir string          `json:"outputDir"`
	// YtDLPPath is a yt-dlp the user located; empty means use the app's own.
	YtDLPPath string `json:"ytdlpPath,omitempty"`
	// LastYtDLPCheck is when the managed yt-dlp last checked for an update
	// successfully.
	LastYtDLPCheck time.Time `json:"lastYtdlpCheck"`
}

// Defaults are the settings on first launch: request.DefaultOptions, Chrome,
// and the Downloads folder.
func Defaults() Settings {
	panic("TODO")
}

// Store reads and writes settings.json.
type Store struct {
	path string
}

// Open returns the store for settings.json in dir, typically MyGo's
// PathUserData.
func Open(dir string) *Store {
	return &Store{path: dir + "/settings.json"}
}

// Load returns the saved settings, falling back to Defaults for a missing or
// unreadable file and for unknown values in it.
func (s *Store) Load() Settings {
	panic("TODO")
}

// Save writes the settings atomically (temp file, then rename).
func (s *Store) Save(v Settings) error {
	panic("TODO")
}
