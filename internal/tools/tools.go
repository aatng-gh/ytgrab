// Package tools finds yt-dlp and ffmpeg, and keeps the managed copy of
// yt-dlp current. It never touches a tool the user located or installed.
package tools

import (
	"context"
	"time"
)

// Dirs are where tools live. Passed in rather than looked up, so tests can
// point them at temporary directories.
type Dirs struct {
	// Bundled is the app's resources/bin, holding the tools it shipped with.
	Bundled string
	// Managed is the writable directory of the updatable yt-dlp copy, in
	// Application Support.
	Managed string
}

// Source says where a tool was found, which decides who updates it.
type Source int

const (
	Missing Source = iota
	Located        // chosen by the user with Locate…
	Managed        // the app's updatable copy
	Bundled        // shipped inside the app
	System         // on PATH, Homebrew, or found through the login shell
)

type Tool struct {
	Path   string
	Source Source
}

// Updatable reports whether the app may update this copy.
func (t Tool) Updatable() bool { return t.Source == Managed }

// Found is the result of a lookup.
type Found struct {
	YtDLP  Tool
	FFmpeg Tool
}

// Ready reports whether a download can run.
func (f Found) Ready() bool { return f.YtDLP.Source != Missing && f.FFmpeg.Source != Missing }

// Find resolves both tools. yt-dlp is looked up in order: located (if
// usable), managed, bundled, system. ffmpeg: bundled, system. Slow when it
// falls back to the login shell, so never call it from the view.
func Find(dirs Dirs, located string) Found {
	panic("TODO")
}

// Seed copies the bundled yt-dlp into dirs.Managed unless a usable copy is
// already there. Copies beside the target, then renames, so a partial file is
// never picked up.
func Seed(dirs Dirs) error {
	panic("TODO")
}

// UpdateInterval is how often the managed copy is checked for updates.
const UpdateInterval = 24 * time.Hour

// UpdateDue reports whether the managed copy should be checked now.
func UpdateDue(lastCheck, now time.Time) bool {
	return now.Sub(lastCheck) >= UpdateInterval
}

// UpdateResult is what an update attempt reports for the view.
type UpdateResult struct {
	OK      bool
	Updated bool
	Message string
}

// Update runs `yt-dlp --update-to stable` on t, which must be Updatable, and
// reads the outcome with ytdlp.ParseUpdate. Bounded by ctx; callers give it
// a few minutes.
func Update(ctx context.Context, t Tool) UpdateResult {
	panic("TODO")
}
