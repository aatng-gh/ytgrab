// Package tools finds yt-dlp and ffmpeg, and keeps the managed copy of
// yt-dlp current. It never touches a tool the user located or installed.
package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aatng-gh/ytgrab/internal/ytdlp"
)

// Dirs are where tools live. Passed in rather than looked up, so tests can
// point them at temporary directories.
type Dirs struct {
	// Bundled is the app's resources/bin, holding the tools it shipped with.
	Bundled string
	// Managed is the writable directory of the updatable yt-dlp copy, in
	// Application Support.
	Managed string
	// Search is where to look for system installs; nil means systemDirs. An app
	// opened from Finder gets a bare PATH, so Homebrew must be named here.
	Search []string
}

// systemDirs are the usual places for command-line tools on macOS.
var systemDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin", "~/.local/bin", "~/bin", "/usr/bin", "/bin"}

// loginShellLookup asks the user's login shell for a command's path, which
// finds pyenv, asdf and conda installs. Replaced in tests.
var loginShellLookup = lookupInLoginShell

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
	var f Found
	if located = strings.TrimSpace(located); located != "" && usable(located) {
		f.YtDLP = Tool{located, Located}
	} else if p := filepath.Join(dirs.Managed, "yt-dlp"); dirs.Managed != "" && usable(p) {
		f.YtDLP = Tool{p, Managed}
	} else {
		f.YtDLP = findInstalled(dirs, "yt-dlp")
	}
	f.FFmpeg = findInstalled(dirs, "ffmpeg")
	return f
}

// findInstalled looks in the bundle, then the system.
func findInstalled(dirs Dirs, name string) Tool {
	if p := filepath.Join(dirs.Bundled, name); dirs.Bundled != "" && usable(p) {
		return Tool{p, Bundled}
	}
	search := dirs.Search
	if search == nil {
		search = systemDirs
	}
	for _, dir := range search {
		if p := filepath.Join(expandHome(dir), name); usable(p) {
			return Tool{p, System}
		}
	}
	if p := loginShellLookup(name); p != "" && usable(p) {
		return Tool{p, System}
	}
	return Tool{}
}

// SearchPath is a PATH for child processes: extra first, then systemDirs and
// the inherited PATH, with ~ expanded, and empty and repeated entries dropped
// (an empty entry would mean the current directory).
func SearchPath(extra ...string) string {
	var dirs []string
	seen := map[string]bool{}
	for _, dir := range append(append(extra, systemDirs...), filepath.SplitList(os.Getenv("PATH"))...) {
		dir = expandHome(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func expandHome(dir string) string {
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return dir
}

// usable reports whether path is an executable regular file. The app is built
// for Apple silicon, where Rosetta runs Intel tools too, so the architecture
// isn't checked.
func usable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// lookupInLoginShell runs `command -v` in the user's login shell, which reads
// their profile. Bounded, because a slow or interactive profile would
// otherwise hang the lookup.
func lookupInLoginShell(name string) string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	// A profile can print a banner first; the path is the last line.
	out := outputWithin(3*time.Second, shell, "-lc", "command -v "+name)
	return out[strings.LastIndexByte(out, '\n')+1:]
}

// outputWithin runs a command and returns its trimmed stdout, or "" if it
// fails or outlasts limit.
func outputWithin(limit time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	out, err := command(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// command is exec.CommandContext for a command that may start children, as
// the bundled yt-dlp does (a launcher and a Python worker): it runs in its own
// process group, and ctx ending kills the whole group, not just the launcher.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	return cmd
}

// versionFile records the version of the yt-dlp beside it: written by
// cmd/fetch-tools into the bundle, and by Seed and Update for the managed copy.
const versionFile = "yt-dlp.version"

// Seed copies the bundled yt-dlp into dirs.Managed when there is no usable
// copy there, or when the bundle's is newer: an app update can ship a newer
// yt-dlp than a copy whose self-updates keep failing. Copies beside the
// target, then renames, so a partial file is never picked up.
func Seed(dirs Dirs) error {
	target := filepath.Join(dirs.Managed, "yt-dlp")
	source := filepath.Join(dirs.Bundled, "yt-dlp")
	if !usable(source) {
		return nil
	}
	bundled := readVersion(dirs.Bundled)
	if usable(target) && !replaceable(bundled, managedVersion(dirs.Managed)) {
		return nil
	}
	if err := install(source, target); err != nil {
		return err
	}
	if bundled != "" {
		return writeVersion(dirs.Managed, bundled)
	}
	return nil
}

// replaceable reports whether a managed copy at version managed should give
// way to the bundled one: it is older, or so broken it can't say what it is.
func replaceable(bundled, managed string) bool {
	if bundled == "" {
		return false
	}
	return managed == "" || ytdlp.Newer(bundled, managed)
}

func readVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, versionFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeVersion(dir, version string) error {
	return os.WriteFile(filepath.Join(dir, versionFile), []byte(version+"\n"), 0o644)
}

// managedVersion is the version of the managed copy in dir: recorded, or, for
// a copy seeded before versions were recorded, what it reports.
func managedVersion(dir string) string {
	if v := readVersion(dir); v != "" {
		return v
	}
	return outputWithin(15*time.Second, filepath.Join(dir, "yt-dlp"), "--version")
}

// install copies source over target atomically, executable.
func install(source, target string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".yt-dlp-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	err = errors.Join(err, out.Chmod(0o755), out.Close())
	if err == nil {
		err = os.Rename(out.Name(), target)
	}
	if err != nil {
		os.Remove(out.Name())
	}
	return err
}

// updateInterval is how often the managed copy is checked for updates.
const updateInterval = 24 * time.Hour

// UpdateDue reports whether the managed copy should be checked now.
func UpdateDue(lastCheck, now time.Time) bool {
	return now.Sub(lastCheck) >= updateInterval
}

// Update runs `yt-dlp --update-to stable` on t, which must be Updatable, and
// reads the outcome with ytdlp.ParseUpdate. yt-dlp's updater checks the
// release's checksums itself. Bounded by ctx; callers give it a few minutes.
func Update(ctx context.Context, t Tool) ytdlp.UpdateResult {
	if !t.Updatable() {
		return ytdlp.UpdateResult{Error: "This yt-dlp is managed outside YTGrab."}
	}
	out, err := command(ctx, t.Path, "--no-config", "--update-to", "stable").CombinedOutput()
	if ctx.Err() != nil {
		return ytdlp.UpdateResult{Error: "the update timed out."}
	}
	code := 0
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		return ytdlp.UpdateResult{Error: "couldn't run yt-dlp: " + err.Error()}
	}
	result := ytdlp.ParseUpdate(string(out), code)
	if result.OK && result.Version != "" {
		writeVersion(filepath.Dir(t.Path), result.Version) // best effort; Seed asks the binary otherwise
	}
	return result
}
