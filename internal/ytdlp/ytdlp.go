// Package ytdlp holds everything that knows yt-dlp's command line: the
// arguments for a request, and what its output lines mean. It runs nothing,
// so all of it is unit tested.
package ytdlp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aatng-gh/ytgrab/internal/request"
)

// Invocation is what a run adds to a request: paths that exist only for it.
type Invocation struct {
	// CookieJar is the temporary file holding pasted cookies, if any.
	CookieJar string
	// FFmpegDir is the directory of the ffmpeg and ffprobe to use.
	FFmpegDir string
}

// progressPrefix starts the lines of the progress template, so ParseLine can
// tell them from the rest of yt-dlp's output.
const progressPrefix = "@@PROG@@"

// Args returns yt-dlp's arguments for req, ending with the URL. They include a
// progress template whose lines ParseLine recognises.
func Args(req request.Request, inv Invocation) []string {
	opts := req.Options
	args := []string{
		"--newline",
		"--progress-template", "download:" + progressPrefix +
			"%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s|%(info.title)s",
		"--concurrent-fragments", "4",
		"-P", req.OutputDir,
	}
	if opts.Playlist {
		// A single video has no playlist fields; the &{}| forms then leave
		// them out instead of writing "NA".
		args = append(args, "--yes-playlist", "-o", "%(playlist_title&{}/|)s%(playlist_index&{} - |)s%(title)s [%(id)s].%(ext)s")
	} else {
		args = append(args, "--no-playlist", "-o", "%(title)s [%(id)s].%(ext)s")
	}
	if inv.FFmpegDir != "" {
		args = append(args, "--ffmpeg-location", inv.FFmpegDir)
	}

	switch opts.Kind {
	case request.Audio:
		args = append(args, "-f", "ba/b", "-x", "--audio-format", string(opts.Audio.Format))
		if !opts.Audio.Format.Lossless() {
			quality := "0"
			if opts.Audio.Bitrate != request.BestBitrate {
				quality = strconv.Itoa(opts.Audio.Bitrate) + "K"
			}
			args = append(args, "--audio-quality", quality)
		}
		if opts.EmbedMetadata {
			args = append(args, "--embed-metadata")
			if opts.Audio.Format.HoldsCoverArt() {
				args = append(args, "--embed-thumbnail")
			}
		}
	default:
		args = append(args, "-f", videoFormat(opts.Video), "--merge-output-format", string(opts.Video.Container))
		if opts.EmbedMetadata {
			args = append(args, "--embed-metadata", "--embed-thumbnail")
		}
		if opts.Subtitles {
			args = append(args, "--write-subs", "--write-auto-subs", "--sub-langs", "en.*,en", "--embed-subs")
		}
	}

	if req.Range != nil {
		start, end := strings.TrimSpace(req.Range.Start), strings.TrimSpace(req.Range.End)
		if start == "" {
			start = "0"
		}
		if end == "" {
			end = "inf"
		}
		args = append(args, "--download-sections", "*"+start+"-"+end, "--force-keyframes-at-cuts")
	}

	switch req.Auth.Mode {
	case request.PastedCookies:
		if inv.CookieJar != "" {
			args = append(args, "--cookies", inv.CookieJar)
		}
	case request.FromBrowser:
		browser := string(req.Auth.Browser)
		if profile := strings.TrimSpace(req.Auth.Profile); profile != "" {
			browser += ":" + profile
		}
		args = append(args, "--cookies-from-browser", browser)
	}

	return append(args, strings.TrimSpace(req.URL))
}

// videoFormat is the -f selector for v. A height is a cap: falling back to an
// uncapped format would make a 360p request download 4K on sparse sites.
func videoFormat(v request.VideoOptions) string {
	filter := ""
	if v.MaxHeight > 0 {
		filter = fmt.Sprintf("[height<=%d]", v.MaxHeight)
	}
	var chain []string
	if v.Container == request.MP4 {
		chain = append(chain, "bv*"+filter+"[ext=mp4]+ba[ext=m4a]")
	}
	chain = append(chain, "bv*"+filter+"+ba", "b"+filter)
	return strings.Join(chain, "/")
}

// CommandLine renders a command for the log, quoted as a shell would need it,
// with the cookie jar's path hidden.
func CommandLine(tool string, args []string) string {
	parts := []string{shellQuote(tool)}
	for i, a := range args {
		if i > 0 && args[i-1] == "--cookies" {
			a = "<pasted cookies>"
		}
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	safe := s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@%+,") == ""
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Line is what one line of yt-dlp's output says. Zero fields say nothing; a
// line can say several things, as "[ExtractAudio] Destination: …" gives both
// a phase and a file. Every line but progress belongs in the log.
type Line struct {
	// Progress is set for lines of the progress template only.
	Progress *Progress
	Phase    Phase
	// File is a file yt-dlp is writing or has written (absolute paths only).
	File string
	// Error is an error yt-dlp reported, without its "ERROR: " prefix.
	Error string
}

type Progress struct {
	// Percent is 0…100, or -1 when unknown.
	Percent float64
	// Speed, ETA and Title are "" when unknown.
	Speed, ETA, Title string
}

// Phase is a step of a download after (or beside) the transfer itself.
type Phase string

const (
	Merging    Phase = "merging"
	Extracting Phase = "extracting"
	Metadata   Phase = "metadata"
	Subtitles  Phase = "subtitles"
)

// ParseLine classifies one line of yt-dlp's stdout or stderr.
func ParseLine(line string) Line {
	line = strings.TrimSpace(line)
	if payload, ok := strings.CutPrefix(line, progressPrefix); ok {
		return Line{Progress: parseProgress(payload)}
	}

	var l Line
	switch {
	case strings.HasPrefix(line, "[Merger]"):
		l.Phase = Merging
	case strings.HasPrefix(line, "[ExtractAudio]"):
		l.Phase = Extracting
	case strings.HasPrefix(line, "[EmbedThumbnail]"), strings.HasPrefix(line, "[Metadata]"):
		l.Phase = Metadata
	case strings.HasPrefix(line, "[info] Downloading subtitles"):
		l.Phase = Subtitles
	}

	for _, marker := range []string{"[download] Destination: ", "[ExtractAudio] Destination: ", `[Merger] Merging formats into "`} {
		if path, ok := strings.CutPrefix(line, marker); ok {
			l.File = absolute(strings.TrimSuffix(path, `"`))
		}
	}
	if rest, ok := strings.CutPrefix(line, "[download] "); ok {
		if path, ok := strings.CutSuffix(rest, " has already been downloaded"); ok {
			l.File = absolute(path)
		}
	}

	// An OS error outside an ERROR: line (a Python traceback, say) explains a
	// failure too; one in a WARNING: line was retried and isn't the cause.
	if msg, ok := strings.CutPrefix(line, "ERROR: "); ok {
		l.Error = msg
	} else if strings.Contains(line, "[Errno ") && !strings.HasPrefix(line, "WARNING:") {
		l.Error = line
	}
	return l
}

func parseProgress(payload string) *Progress {
	parts := strings.SplitN(payload, "|", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	p := &Progress{Percent: -1, Speed: known(parts[1]), ETA: known(parts[2]), Title: known(parts[3])}
	if v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[0]), "%"), 64); err == nil {
		p.Percent = v
	}
	return p
}

// known turns yt-dlp's placeholders for missing values into "".
func known(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "N/A", "NA", "none", "Unknown":
		return ""
	}
	return s
}

func absolute(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return ""
}

// UpdateResult is the outcome of `yt-dlp --update-to stable`.
type UpdateResult struct {
	OK bool
	// Updated is true when a new version was installed.
	Updated bool
	// Version is the version now installed, such as "2026.10.26", when known.
	Version string
	// Error says what went wrong when !OK.
	Error string
}

// versionPattern finds the version in "stable@2026.10.26 from yt-dlp/yt-dlp".
var versionPattern = regexp.MustCompile(`@(\d+(?:\.\d+)+)`)

// ParseUpdate reads the updater's combined output and exit code.
func ParseUpdate(output string, exitCode int) UpdateResult {
	var lines []string
	for _, l := range strings.Split(output, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	last := func(match func(string) bool) string {
		for i := len(lines) - 1; i >= 0; i-- {
			if match(lines[i]) {
				return lines[i]
			}
		}
		return ""
	}

	version := func(l string) string {
		if m := versionPattern.FindStringSubmatch(l); m != nil {
			return m[1]
		}
		return ""
	}

	if exitCode == 0 {
		if l := last(func(l string) bool { return strings.HasPrefix(l, "Updated yt-dlp") }); l != "" {
			return UpdateResult{OK: true, Updated: true, Version: version(l)}
		}
		if l := last(func(l string) bool { return strings.HasPrefix(l, "yt-dlp is up to date") }); l != "" {
			return UpdateResult{OK: true, Version: version(l)}
		}
	}
	if l := last(func(l string) bool { return strings.HasPrefix(l, "ERROR:") }); l != "" {
		return UpdateResult{Error: strings.TrimSpace(strings.TrimPrefix(l, "ERROR:"))}
	}
	return UpdateResult{Error: fmt.Sprintf("yt-dlp exited with code %d.", exitCode)}
}

// Newer reports whether version a is later than b. yt-dlp's versions are
// dates with optional build numbers (2026.10.26, 2026.10.26.1), compared part
// by part as numbers. Unknown or malformed versions are never newer.
func Newer(a, b string) bool {
	pa, okA := versionParts(a)
	pb, okB := versionParts(b)
	if !okA || !okB {
		return false
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func versionParts(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	var parts []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, false
		}
		parts = append(parts, n)
	}
	return parts, true
}
