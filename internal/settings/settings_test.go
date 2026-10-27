package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aatng-gh/ytgrab/internal/request"
)

func TestDefaults(t *testing.T) {
	home, _ := os.UserHomeDir()
	d := Defaults()
	if d.Options != request.DefaultOptions() || d.Browser != "chrome" ||
		d.OutputDir != filepath.Join(home, "Downloads") || d.YtDLPPath != "" {
		t.Errorf("Defaults = %+v", d)
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	if got := Open(t.TempDir()).Load(); got != Defaults() {
		t.Errorf("Load = %+v, want defaults", got)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet-created")
	s := Open(dir)
	want := Settings{
		Options: request.Options{
			Kind:      request.Audio,
			Video:     request.VideoOptions{MaxHeight: 720, Container: request.MKV},
			Audio:     request.AudioOptions{Format: request.Opus, Bitrate: 192},
			Playlist:  true,
			Subtitles: true,
		},
		Browser:        "firefox",
		OutputDir:      "/Volumes/Media",
		YtDLPPath:      "/opt/homebrew/bin/yt-dlp",
		LastYtDLPCheck: time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC),
	}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got := s.Load()
	if !got.LastYtDLPCheck.Equal(want.LastYtDLPCheck) {
		t.Errorf("LastYtDLPCheck = %v, want %v", got.LastYtDLPCheck, want.LastYtDLPCheck)
	}
	got.LastYtDLPCheck = want.LastYtDLPCheck
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only settings.json", len(entries))
	}
}

func TestCorruptFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{not json"), 0o600)
	if got := Open(dir).Load(); got != Defaults() {
		t.Errorf("Load = %+v, want defaults", got)
	}
}

func TestUnknownValuesFallBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{
		"options": {
			"kind": "hologram",
			"video": {"maxHeight": 999, "container": "avi"},
			"audio": {"format": "aiff", "bitrate": 64},
			"playlist": true
		},
		"browser": "netscape",
		"outputDir": "/Volumes/Media"
	}`), 0o600)

	got := Open(dir).Load()
	d := Defaults()
	want := d
	want.Options.Playlist = true
	want.OutputDir = "/Volumes/Media"
	// Fields missing from the file (embedMetadata) keep their defaults too.
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestEmptyOutputDirFallsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"outputDir": ""}`), 0o600)
	if got := Open(dir).Load(); got.OutputDir != Defaults().OutputDir {
		t.Errorf("OutputDir = %q", got.OutputDir)
	}
}
