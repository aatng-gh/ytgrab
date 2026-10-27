package ytdlp

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/aatng-gh/ytgrab/internal/request"
)

func base() request.Request {
	return request.Request{
		URL:       " https://www.youtube.com/watch?v=abc ",
		Options:   request.DefaultOptions(),
		OutputDir: "/Users/me/Downloads",
	}
}

// has reports whether seq appears in args as consecutive elements.
func has(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func TestArgsVideo(t *testing.T) {
	args := Args(base(), Invocation{FFmpegDir: "/app/bin"})

	for _, seq := range [][]string{
		{"--newline"},
		{"--no-playlist"},
		{"--concurrent-fragments", "4"},
		{"-P", "/Users/me/Downloads"},
		{"-o", "%(title)s [%(id)s].%(ext)s"},
		{"--ffmpeg-location", "/app/bin"},
		{"-f", "bv*[height<=1080][ext=mp4]+ba[ext=m4a]/bv*[height<=1080]+ba/b[height<=1080]"},
		{"--merge-output-format", "mp4"},
		{"--embed-metadata", "--embed-thumbnail"},
	} {
		if !has(args, seq...) {
			t.Errorf("args lack %q:\n%q", seq, args)
		}
	}
	if args[len(args)-1] != "https://www.youtube.com/watch?v=abc" {
		t.Errorf("args end with %q, want the trimmed URL", args[len(args)-1])
	}
	if i := slices.Index(args, "--progress-template"); i < 0 || !strings.HasPrefix(args[i+1], "download:"+progressPrefix) {
		t.Errorf("args lack the progress template: %q", args)
	}
	for _, flag := range []string{"--write-subs", "--cookies", "--cookies-from-browser", "--download-sections", "-x"} {
		if slices.Contains(args, flag) {
			t.Errorf("args contain %s unasked", flag)
		}
	}
}

func TestVideoFormat(t *testing.T) {
	tests := []struct {
		height    int
		container request.Container
		want      string
	}{
		{360, request.MP4, "bv*[height<=360][ext=mp4]+ba[ext=m4a]/bv*[height<=360]+ba/b[height<=360]"},
		{360, request.MKV, "bv*[height<=360]+ba/b[height<=360]"},
		{0, request.MP4, "bv*[ext=mp4]+ba[ext=m4a]/bv*+ba/b"},
		{0, request.MKV, "bv*+ba/b"},
	}
	for _, tt := range tests {
		got := videoFormat(request.VideoOptions{MaxHeight: tt.height, Container: tt.container})
		if got != tt.want {
			t.Errorf("videoFormat(%d, %s) = %q, want %q", tt.height, tt.container, got, tt.want)
		}
	}
}

func TestArgsOptions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*request.Request)
		want   [][]string
		absent []string
	}{
		{"playlist", func(r *request.Request) { r.Options.Playlist = true },
			[][]string{{"--yes-playlist"}, {"-o", "%(playlist_title&{}/|)s%(playlist_index&{} - |)s%(title)s [%(id)s].%(ext)s"}},
			[]string{"--no-playlist"}},
		{"no extras", func(r *request.Request) { r.Options.EmbedMetadata = false },
			nil, []string{"--embed-metadata", "--embed-thumbnail"}},
		{"subtitles", func(r *request.Request) { r.Options.Subtitles = true },
			[][]string{{"--write-subs", "--write-auto-subs", "--sub-langs", "en.*,en", "--embed-subs"}}, nil},
		{"mkv", func(r *request.Request) { r.Options.Video.Container = request.MKV },
			[][]string{{"--merge-output-format", "mkv"}}, nil},
		{"audio", func(r *request.Request) { r.Options.Kind = request.Audio },
			[][]string{{"-f", "ba/b"}, {"-x", "--audio-format", "m4a"}, {"--audio-quality", "0"},
				{"--embed-metadata", "--embed-thumbnail"}},
			[]string{"--merge-output-format"}},
		{"audio bitrate", func(r *request.Request) {
			r.Options.Kind = request.Audio
			r.Options.Audio = request.AudioOptions{Format: request.MP3, Bitrate: 192}
		}, [][]string{{"--audio-format", "mp3"}, {"--audio-quality", "192K"}}, nil},
		{"lossless audio", func(r *request.Request) {
			r.Options.Kind = request.Audio
			r.Options.Audio = request.AudioOptions{Format: request.FLAC, Bitrate: 320}
		}, [][]string{{"--audio-format", "flac"}}, []string{"--audio-quality"}},
		{"wav has no cover art", func(r *request.Request) {
			r.Options.Kind = request.Audio
			r.Options.Audio.Format = request.WAV
		}, [][]string{{"--embed-metadata"}}, []string{"--embed-thumbnail"}},
		{"audio ignores subtitles", func(r *request.Request) {
			r.Options.Kind = request.Audio
			r.Options.Subtitles = true
		}, nil, []string{"--write-subs"}},
		{"range", func(r *request.Request) { r.Range = &request.Range{Start: " 1:30 ", End: "2:00"} },
			[][]string{{"--download-sections", "*1:30-2:00", "--force-keyframes-at-cuts"}}, nil},
		{"range to the end", func(r *request.Request) { r.Range = &request.Range{Start: "1:30"} },
			[][]string{{"--download-sections", "*1:30-inf"}}, nil},
		{"range from the start", func(r *request.Request) { r.Range = &request.Range{End: "2:00"} },
			[][]string{{"--download-sections", "*0-2:00"}}, nil},
		{"browser", func(r *request.Request) {
			r.Auth = request.Auth{Mode: request.FromBrowser, Browser: "firefox"}
		}, [][]string{{"--cookies-from-browser", "firefox"}}, nil},
		{"browser profile", func(r *request.Request) {
			r.Auth = request.Auth{Mode: request.FromBrowser, Browser: "chrome", Profile: " Profile 1 "}
		}, [][]string{{"--cookies-from-browser", "chrome:Profile 1"}}, nil},
		{"browser cookies ignore pasted ones", func(r *request.Request) {
			r.Auth = request.Auth{Mode: request.NoAuth, Cookies: "x", Browser: "chrome"}
		}, nil, []string{"--cookies", "--cookies-from-browser"}},
	}
	for _, tt := range tests {
		r := base()
		tt.change(&r)
		args := Args(r, Invocation{})
		for _, seq := range tt.want {
			if !has(args, seq...) {
				t.Errorf("%s: args lack %q:\n%q", tt.name, seq, args)
			}
		}
		for _, flag := range tt.absent {
			if slices.Contains(args, flag) {
				t.Errorf("%s: args contain %s:\n%q", tt.name, flag, args)
			}
		}
	}
}

func TestArgsCookieJar(t *testing.T) {
	r := base()
	r.Auth = request.Auth{Mode: request.PastedCookies, Cookies: "…"}
	args := Args(r, Invocation{CookieJar: "/tmp/YTGrab-1-x/cookies.txt"})
	if !has(args, "--cookies", "/tmp/YTGrab-1-x/cookies.txt") {
		t.Fatalf("args lack the jar: %q", args)
	}
	line := CommandLine("yt-dlp", args)
	if !strings.Contains(line, "--cookies '<pasted cookies>'") || strings.Contains(line, "/tmp/YTGrab-1-x") {
		t.Errorf("CommandLine shows the jar: %s", line)
	}
	if !slices.Contains(args, "/tmp/YTGrab-1-x/cookies.txt") {
		t.Error("CommandLine changed its argument")
	}
}

func TestCommandLine(t *testing.T) {
	got := CommandLine("/bin/yt-dlp", []string{"-o", "%(title)s [%(id)s].%(ext)s", "it's", "plain"})
	want := `/bin/yt-dlp -o '%(title)s [%(id)s].%(ext)s' 'it'\''s' plain`
	if got != want {
		t.Errorf("CommandLine = %s, want %s", got, want)
	}
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		line string
		want Line
	}{
		{progressPrefix + " 42.5%|1.20MiB/s|00:10|A | B",
			Line{Progress: &Progress{Percent: 42.5, Speed: "1.20MiB/s", ETA: "00:10", Title: "A | B"}}},
		{progressPrefix + "N/A|N/A|NA|T",
			Line{Progress: &Progress{Percent: -1, Title: "T"}}},
		{"[download] Destination: /d/a.f137.mp4", Line{File: "/d/a.f137.mp4"}},
		{"[download] Destination: relative.mp4", Line{}},
		{`[Merger] Merging formats into "/d/a.mp4"`, Line{Phase: Merging, File: "/d/a.mp4"}},
		{"[ExtractAudio] Destination: /d/a.m4a", Line{Phase: Extracting, File: "/d/a.m4a"}},
		{"[ExtractAudio] Not converting audio /d/a.m4a; file is already in target format m4a",
			Line{Phase: Extracting}},
		{"[download] /d/a.mp4 has already been downloaded", Line{File: "/d/a.mp4"}},
		{"[EmbedThumbnail] ffmpeg: Adding thumbnail to \"/d/a.mp4\"", Line{Phase: Metadata}},
		{"[Metadata] Adding metadata to \"/d/a.mp4\"", Line{Phase: Metadata}},
		{"[info] Downloading subtitles: en", Line{Phase: Subtitles}},
		{"ERROR: [youtube] abc: Video unavailable", Line{Error: "[youtube] abc: Video unavailable"}},
		{"OSError: [Errno 28] No space left on device", Line{Error: "OSError: [Errno 28] No space left on device"}},
		{"[youtube] abc: Downloading webpage", Line{}},
		{"WARNING: [youtube] abc: [Errno 54] Connection reset by peer. Retrying (1/3)...", Line{}},
	}
	for _, tt := range tests {
		got := ParseLine(tt.line)
		if !equalLine(got, tt.want) {
			t.Errorf("ParseLine(%q) = %s, want %s", tt.line, show(got), show(tt.want))
		}
	}
}

func equalLine(a, b Line) bool {
	if (a.Progress == nil) != (b.Progress == nil) || (a.Progress != nil && *a.Progress != *b.Progress) {
		return false
	}
	return a.Phase == b.Phase && a.File == b.File && a.Error == b.Error
}

func show(l Line) string {
	if l.Progress != nil {
		return fmt.Sprintf("%+v progress=%+v", l, *l.Progress)
	}
	return fmt.Sprintf("%+v", l)
}

func TestParseUpdate(t *testing.T) {
	tests := []struct {
		name   string
		output string
		code   int
		want   UpdateResult
	}{
		{"updated", `Current version: stable@2026.08.19 from yt-dlp/yt-dlp
Latest version: stable@2026.10.26 from yt-dlp/yt-dlp
Current Build Hash: 0f19
Updating to stable@2026.10.26 from yt-dlp/yt-dlp ...
Updated yt-dlp to stable@2026.10.26 from yt-dlp/yt-dlp
`, 0, UpdateResult{OK: true, Updated: true, Version: "2026.10.26"}},
		{"current", `Latest version: stable@2026.10.26 from yt-dlp/yt-dlp
yt-dlp is up to date (stable@2026.10.26 from yt-dlp/yt-dlp)
`, 0, UpdateResult{OK: true, Version: "2026.10.26"}},
		{"nightly", "yt-dlp is up to date (nightly@2026.10.27.001 from yt-dlp/yt-dlp-nightly-builds)\n", 0,
			UpdateResult{OK: true, Version: "2026.10.27.001"}},
		{"offline", "ERROR: Unable to obtain version info (<urlopen error [Errno 8] nodename nor servname provided>)\n", 1,
			UpdateResult{Error: "Unable to obtain version info (<urlopen error [Errno 8] nodename nor servname provided>)"}},
		{"silent failure", "", 1, UpdateResult{Error: "yt-dlp exited with code 1."}},
		{"success without a verdict", "something else\n", 0, UpdateResult{Error: "yt-dlp exited with code 0."}},
	}
	for _, tt := range tests {
		if got := ParseUpdate(tt.output, tt.code); got != tt.want {
			t.Errorf("%s: ParseUpdate = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestNewer(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"2026.10.26", "2026.08.19", true},
		{"2026.08.19", "2026.10.26", false},
		{"2026.10.26", "2026.10.26", false},
		{"2026.10.26.1", "2026.10.26", true},
		{"2026.10.9", "2026.10.26", false}, // numeric, not lexical
		{"", "2026.10.26", false},
		{"2026.10.26", "", false},
		{"garbage", "2026.10.26", false},
	}
	for _, tt := range tests {
		if got := Newer(tt.a, tt.b); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
