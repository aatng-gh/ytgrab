// Package ytdlp holds everything that knows yt-dlp's command line: the
// arguments for a request, and what its output lines mean. It runs nothing,
// so all of it is unit tested. If go-ytdlp is adopted, it replaces the
// insides of this package, not its API.
package ytdlp

import "github.com/aatng-gh/ytgrab/internal/request"

// Invocation is what a run adds to a request: paths that exist only for it.
type Invocation struct {
	// CookieJar is the temporary file holding pasted cookies, if any.
	CookieJar string
	// FFmpegDir is the directory of the ffmpeg and ffprobe to use.
	FFmpegDir string
}

// Args returns yt-dlp's arguments for req, ending with the URL. They include a
// progress template whose lines ParseLine recognises.
func Args(req request.Request, inv Invocation) []string {
	panic("TODO")
}

// Redact replaces the cookie jar's path in args, for the log.
func Redact(args []string) []string {
	panic("TODO")
}

// Line is what one line of yt-dlp's output says. Most lines say nothing the
// app acts on; every line still goes to the log, except progress lines.
type Line struct {
	Kind LineKind

	// Progress lines.
	Percent  float64 // -1 when unknown
	Speed    string  // "" when unknown
	ETA      string  // "" when unknown
	Title    string
	// Phase lines.
	Phase Phase
	// File lines: the file yt-dlp is writing or has written.
	File string
	// Error lines.
	Error string
}

type LineKind int

const (
	Other LineKind = iota
	Progress
	PhaseChange
	FileWritten
	Error
)

// Phase is a step of a download after the transfer itself.
type Phase string

const (
	Merging    Phase = "merging"
	Extracting Phase = "extracting"
	Metadata   Phase = "metadata"
	Subtitles  Phase = "subtitles"
)

// ParseLine classifies one line of yt-dlp's stdout or stderr.
func ParseLine(line string) Line {
	panic("TODO")
}

// UpdateResult is the outcome of `yt-dlp --update-to stable`.
type UpdateResult struct {
	OK bool
	// Updated is true when a new version was installed, false when already
	// current or failed.
	Updated bool
	// Message is the line worth showing: "Updated yt-dlp to …", "yt-dlp is up
	// to date (…)", or the error.
	Message string
}

// ParseUpdate reads the updater's combined output and exit code.
func ParseUpdate(output string, exitCode int) UpdateResult {
	panic("TODO")
}
