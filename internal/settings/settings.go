// Package settings remembers the user's choices in a JSON file. It holds
// nothing per-download or sensitive: no URL, cookies, range or browser profile.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	return Settings{
		Options:   request.DefaultOptions(),
		Browser:   "chrome",
		OutputDir: filepath.Join(home, "Downloads"),
	}
}

// Store reads and writes settings.json.
type Store struct {
	path string
}

// Open returns the store for settings.json in dir, typically MyGo's
// PathUserData.
func Open(dir string) *Store {
	return &Store{path: filepath.Join(dir, "settings.json")}
}

// Load returns the saved settings, falling back to Defaults for a missing or
// unreadable file and for unknown values in it.
func (s *Store) Load() Settings {
	d := Defaults()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return d
	}
	v := d
	if err := json.Unmarshal(data, &v); err != nil {
		return d
	}
	v.Options = v.Options.Normalized()
	if !slices.Contains(request.Browsers, v.Browser) {
		v.Browser = d.Browser
	}
	if v.OutputDir == "" {
		v.OutputDir = d.OutputDir
	}
	return v
}

// Save writes the settings atomically (temp file, then rename).
func (s *Store) Save(v Settings) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	err = errors.Join(err, f.Close())
	if err == nil {
		err = os.Rename(f.Name(), s.path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
