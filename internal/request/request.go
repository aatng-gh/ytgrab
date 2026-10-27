// Package request describes what the user asked to download, independent of
// how yt-dlp is told to do it (package ytdlp) or how it runs (package job).
package request

// Request is one download, as the form describes it.
type Request struct {
	URL string
	// Options are the choices remembered between launches (package settings).
	Options Options
	// Range limits the download to part of the media; nil means all of it.
	Range *Range
	Auth  Auth
	// OutputDir is where files are written.
	OutputDir string
}

// Options are the format choices, kept across launches. Nothing in here is
// per-download or sensitive.
type Options struct {
	Kind  Kind
	Video VideoOptions
	Audio AudioOptions
	// Playlist downloads the whole playlist when the URL is one.
	Playlist bool
	// EmbedMetadata embeds metadata and, where the format allows, cover art.
	EmbedMetadata bool
	// Subtitles embeds English subtitles (video only).
	Subtitles bool
}

// DefaultOptions are the choices before the user has made any.
func DefaultOptions() Options {
	return Options{
		Kind:          Video,
		Video:         VideoOptions{MaxHeight: 1080, Container: MP4},
		Audio:         AudioOptions{Format: M4A, Bitrate: BestBitrate},
		EmbedMetadata: true,
	}
}

type Kind string

const (
	Video Kind = "video"
	Audio Kind = "audio"
)

type VideoOptions struct {
	// MaxHeight caps the resolution, in pixels; 0 means best available. A cap
	// is never exceeded, even when nothing smaller exists.
	MaxHeight int
	Container Container
}

// Heights are the caps the form offers, best first.
var Heights = []int{0, 2160, 1440, 1080, 720, 480, 360}

type Container string

const (
	MP4 Container = "mp4"
	MKV Container = "mkv"
)

type AudioOptions struct {
	Format AudioFormat
	// Bitrate in kbps for lossy formats; BestBitrate leaves it to the source.
	Bitrate int
}

const BestBitrate = 0

// Bitrates are the choices the form offers, best first.
var Bitrates = []int{BestBitrate, 320, 256, 192, 128}

type AudioFormat string

const (
	MP3  AudioFormat = "mp3"
	M4A  AudioFormat = "m4a"
	Opus AudioFormat = "opus"
	FLAC AudioFormat = "flac"
	WAV  AudioFormat = "wav"
)

// Lossless formats ignore Bitrate.
func (f AudioFormat) Lossless() bool { return f == FLAC || f == WAV }

// HoldsCoverArt reports whether the format can embed a thumbnail; WAV can't,
// and asking for one fails the run after the download has finished.
func (f AudioFormat) HoldsCoverArt() bool { return f != WAV }

// Range is a span of the media. Either end may be empty, meaning the start or
// the end of the media. Timestamps are h:mm:ss, m:ss or seconds, optionally
// fractional; a leading minus counts from the end.
type Range struct {
	Start, End string
}

type Auth struct {
	Mode AuthMode
	// Cookies is a pasted Netscape cookie jar (Mode == PastedCookies). It is
	// never persisted.
	Cookies string
	// Browser and Profile name where to read cookies (Mode == FromBrowser).
	// Profile is optional and never persisted.
	Browser Browser
	Profile string
}

type AuthMode string

const (
	NoAuth        AuthMode = "none"
	PastedCookies AuthMode = "cookies"
	FromBrowser   AuthMode = "browser"
)

// Browser is a value yt-dlp's --cookies-from-browser accepts.
type Browser string

var Browsers = []Browser{"chrome", "safari", "firefox", "edge", "brave", "chromium", "opera", "vivaldi"}

// Problem is a reason a request can't start, tied to the field to show it on.
type Problem struct {
	Field   Field
	Message string
}

type Field string

const (
	FieldURL     Field = "url"
	FieldRange   Field = "range"
	FieldCookies Field = "cookies"
	FieldOutput  Field = "output"
)

// Validate returns what stops the request from starting; none means ready.
//
// A request is ready when the URL is http(s), a range has at least one valid
// timestamp with start before end when both are absolute, pasted cookies hold
// at least one cookie, and OutputDir is set.
func (r Request) Validate() []Problem {
	panic("TODO")
}

// ParseTimestamp reads a Range endpoint. fromEnd is true for negative
// timestamps, whose seconds count back from the end of the media.
func ParseTimestamp(s string) (seconds float64, fromEnd bool, ok bool) {
	panic("TODO")
}
