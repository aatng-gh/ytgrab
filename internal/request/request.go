// Package request describes what the user asked to download, independent of
// how yt-dlp is told to do it (package ytdlp) or how it runs (package job).
package request

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/aatng-gh/ytgrab/internal/cookies"
)

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
	Kind  Kind         `json:"kind"`
	Video VideoOptions `json:"video"`
	Audio AudioOptions `json:"audio"`
	// Playlist downloads the whole playlist when the URL is one.
	Playlist bool `json:"playlist"`
	// EmbedMetadata embeds metadata and, where the format allows, cover art.
	EmbedMetadata bool `json:"embedMetadata"`
	// Subtitles embeds English subtitles (video only).
	Subtitles bool `json:"subtitles"`
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

// Normalized replaces values the app doesn't offer, as from an older or
// hand-edited settings file, with the defaults.
func (o Options) Normalized() Options {
	d := DefaultOptions()
	if !slices.Contains(Kinds, o.Kind) {
		o.Kind = d.Kind
	}
	if !slices.Contains(Heights, o.Video.MaxHeight) {
		o.Video.MaxHeight = d.Video.MaxHeight
	}
	if !slices.Contains(Containers, o.Video.Container) {
		o.Video.Container = d.Video.Container
	}
	if !slices.Contains(AudioFormats, o.Audio.Format) {
		o.Audio.Format = d.Audio.Format
	}
	if !slices.Contains(Bitrates, o.Audio.Bitrate) {
		o.Audio.Bitrate = d.Audio.Bitrate
	}
	return o
}

type Kind string

const (
	Video Kind = "video"
	Audio Kind = "audio"
)

var Kinds = []Kind{Video, Audio}

type VideoOptions struct {
	// MaxHeight caps the resolution, in pixels; 0 means best available. A cap
	// is never exceeded, even when nothing smaller exists.
	MaxHeight int       `json:"maxHeight"`
	Container Container `json:"container"`
}

// Heights are the caps the form offers, best first.
var Heights = []int{0, 2160, 1440, 1080, 720, 480, 360}

type Container string

const (
	MP4 Container = "mp4"
	MKV Container = "mkv"
)

var Containers = []Container{MP4, MKV}

type AudioOptions struct {
	Format AudioFormat `json:"format"`
	// Bitrate in kbps for lossy formats; BestBitrate leaves it to the source.
	Bitrate int `json:"bitrate"`
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

var AudioFormats = []AudioFormat{MP3, M4A, Opus, FLAC, WAV}

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
func (r Request) Validate() []Problem {
	var problems []Problem
	add := func(f Field, msg string) { problems = append(problems, Problem{f, msg}) }

	url := strings.TrimSpace(r.URL)
	switch {
	case url == "":
		add(FieldURL, "Paste a link to download.")
	case !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://"):
		add(FieldURL, "The link must start with http:// or https://.")
	}
	if r.Range != nil {
		if msg := r.Range.problem(); msg != "" {
			add(FieldRange, msg)
		}
	}
	if r.Auth.Mode == PastedCookies && cookies.Count(r.Auth.Cookies) == 0 {
		add(FieldCookies, "No cookies recognised. Paste a Netscape cookies.txt export.")
	}
	if r.OutputDir == "" {
		add(FieldOutput, "Choose a folder to save to.")
	}
	return problems
}

// problem explains what is wrong with the range, or returns "".
func (rg Range) problem() string {
	start, end := strings.TrimSpace(rg.Start), strings.TrimSpace(rg.End)
	if start == "" && end == "" {
		return "Enter a start, an end, or both."
	}
	s, sFromEnd, sOK := parseTimestamp(start)
	e, eFromEnd, eOK := parseTimestamp(end)
	if (start != "" && !sOK) || (end != "" && !eOK) {
		return "Use timestamps like 1:30 or 12:04:00."
	}
	// yt-dlp accepts an inverted range silently and writes nothing, so catch
	// it here. Ends counted from the end of the media can't be compared.
	if start != "" && end != "" && !sFromEnd && !eFromEnd && s >= e {
		return "The start must come before the end."
	}
	return ""
}

var timestampPattern = regexp.MustCompile(`^-?\d+(:[0-5]\d){0,2}(\.\d+)?$`)

// parseTimestamp reads a Range endpoint. fromEnd is true for negative
// timestamps, whose seconds count back from the end of the media.
func parseTimestamp(s string) (seconds float64, fromEnd bool, ok bool) {
	s = strings.TrimSpace(s)
	if !timestampPattern.MatchString(s) {
		return 0, false, false
	}
	s, fromEnd = strings.CutPrefix(s, "-")
	for _, part := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, false, false
		}
		seconds = seconds*60 + v
	}
	return seconds, fromEnd, true
}
