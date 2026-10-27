package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aatng-gh/ytgrab/internal/ytdlp"
)

// layout makes bundled, managed and system directories under a temp dir and
// stops the login-shell fallback from finding this machine's real tools.
func layout(t *testing.T) Dirs {
	t.Helper()
	root := t.TempDir()
	d := Dirs{
		Bundled: filepath.Join(root, "bundled"),
		Managed: filepath.Join(root, "managed"),
		Search:  []string{filepath.Join(root, "system")},
	}
	for _, dir := range []string{d.Bundled, d.Search[0]} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	orig := loginShellLookup
	loginShellLookup = func(string) string { return "" }
	t.Cleanup(func() { loginShellLookup = orig })
	return d
}

// script writes an executable shell script.
func script(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindOrder(t *testing.T) {
	d := layout(t)
	if f := Find(d, ""); f.YtDLP.Source != Missing || f.FFmpeg.Source != Missing || f.Ready() {
		t.Fatalf("empty layout: %+v", f)
	}

	system := script(t, filepath.Join(d.Search[0], "yt-dlp"), "")
	script(t, filepath.Join(d.Search[0], "ffmpeg"), "")
	if f := Find(d, ""); f.YtDLP != (Tool{system, System}) || f.FFmpeg.Source != System || !f.Ready() {
		t.Errorf("system only: %+v", f)
	}

	bundled := script(t, filepath.Join(d.Bundled, "yt-dlp"), "")
	bundledFFmpeg := script(t, filepath.Join(d.Bundled, "ffmpeg"), "")
	if f := Find(d, ""); f.YtDLP != (Tool{bundled, Bundled}) || f.FFmpeg != (Tool{bundledFFmpeg, Bundled}) {
		t.Errorf("with bundle: %+v", f)
	}

	managed := script(t, filepath.Join(d.Managed, "yt-dlp"), "")
	if f := Find(d, ""); f.YtDLP != (Tool{managed, Managed}) || !f.YtDLP.Updatable() {
		t.Errorf("with managed copy: %+v", f)
	}

	located := script(t, filepath.Join(t.TempDir(), "my-yt-dlp"), "")
	if f := Find(d, located); f.YtDLP != (Tool{located, Located}) || f.YtDLP.Updatable() {
		t.Errorf("located: %+v", f)
	}
	if f := Find(d, "/nonexistent/yt-dlp"); f.YtDLP.Source != Managed {
		t.Errorf("a missing located path should fall through: %+v", f)
	}
}

func TestFindSkipsUnusable(t *testing.T) {
	d := layout(t)
	os.WriteFile(filepath.Join(d.Bundled, "yt-dlp"), []byte("not executable"), 0o644)
	os.Mkdir(filepath.Join(d.Search[0], "yt-dlp"), 0o755)
	if f := Find(d, ""); f.YtDLP.Source != Missing {
		t.Errorf("found %+v", f.YtDLP)
	}
}

func TestFindFallsBackToLoginShell(t *testing.T) {
	d := layout(t)
	pyenv := script(t, filepath.Join(t.TempDir(), "yt-dlp"), "")
	loginShellLookup = func(name string) string {
		if name == "yt-dlp" {
			return pyenv
		}
		return ""
	}
	if f := Find(d, ""); f.YtDLP != (Tool{pyenv, System}) {
		t.Errorf("found %+v", f.YtDLP)
	}
}

func TestExpandsHome(t *testing.T) {
	d := layout(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	local := script(t, filepath.Join(home, ".local", "bin", "yt-dlp"), "")
	d.Search = []string{"~/.local/bin"}
	if f := Find(d, ""); f.YtDLP.Path != local {
		t.Errorf("found %+v", f.YtDLP)
	}
}

func TestSeed(t *testing.T) {
	d := layout(t)
	if err := Seed(d); err != nil {
		t.Fatalf("Seed without a bundle: %v", err)
	}
	if _, err := os.Stat(d.Managed); !os.IsNotExist(err) {
		t.Error("Seed without a bundle created the managed directory")
	}

	script(t, filepath.Join(d.Bundled, "yt-dlp"), "echo bundled")
	if err := Seed(d); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(d.Managed, "yt-dlp")
	if data, _ := os.ReadFile(managed); !strings.Contains(string(data), "echo bundled") {
		t.Errorf("managed copy holds %q", data)
	}
	if info, err := os.Stat(managed); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("managed copy isn't executable: %v %v", info, err)
	}

	// An existing, possibly updated copy is kept.
	script(t, managed, "echo updated")
	if err := Seed(d); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(managed); !strings.Contains(string(data), "echo updated") {
		t.Errorf("Seed replaced an existing copy: %q", data)
	}
	if entries, _ := os.ReadDir(d.Managed); len(entries) != 1 {
		t.Errorf("managed directory holds %d entries, want 1", len(entries))
	}
}

func TestUpdate(t *testing.T) {
	d := layout(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	path := script(t, filepath.Join(d.Managed, "yt-dlp"), `echo "$@" > `+argsFile+`
echo "Latest version: stable@2026.10.26 from yt-dlp/yt-dlp"
echo "Updated yt-dlp to stable@2026.10.26 from yt-dlp/yt-dlp" >&2`)

	got := Update(context.Background(), Tool{path, Managed})
	want := ytdlp.UpdateResult{OK: true, Updated: true, Version: "2026.10.26"}
	if got != want {
		t.Errorf("Update = %+v, want %+v", got, want)
	}
	if args, _ := os.ReadFile(argsFile); strings.TrimSpace(string(args)) != "--no-config --update-to stable" {
		t.Errorf("yt-dlp ran with %q", args)
	}
}

func TestUpdateFailures(t *testing.T) {
	d := layout(t)
	failing := script(t, filepath.Join(d.Managed, "yt-dlp"), `echo "ERROR: Unable to obtain version info" >&2; exit 1`)
	if got := Update(context.Background(), Tool{failing, Managed}); got.OK || !strings.Contains(got.Error, "Unable to obtain version info") {
		t.Errorf("failing updater: %+v", got)
	}

	if got := Update(context.Background(), Tool{failing, System}); got.OK || !strings.Contains(got.Error, "outside YTGrab") {
		t.Errorf("system tool: %+v", got)
	}

	hanging := script(t, filepath.Join(t.TempDir(), "yt-dlp"), "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := Update(ctx, Tool{hanging, Managed}); got.OK || !strings.Contains(got.Error, "timed out") {
		t.Errorf("hanging updater: %+v", got)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Update outlived its context")
	}
}

func TestUpdateDue(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if !UpdateDue(time.Time{}, now) || !UpdateDue(now.Add(-25*time.Hour), now) || UpdateDue(now.Add(-time.Hour), now) {
		t.Error("UpdateDue disagrees with a one-day interval")
	}
}

func TestSearchPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin::/custom/bin")
	got := strings.Split(SearchPath("/app/bin"), ":")
	if got[0] != "/app/bin" {
		t.Errorf("first entry %q, want the extra directory", got[0])
	}
	seen := map[string]bool{}
	for _, dir := range got {
		if dir == "" || strings.HasPrefix(dir, "~") {
			t.Errorf("unusable entry %q in %q", dir, got)
		}
		if seen[dir] {
			t.Errorf("duplicate entry %q in %q", dir, got)
		}
		seen[dir] = true
	}
	for _, want := range []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/custom/bin"} {
		if !seen[want] {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}

// versioned writes a fake yt-dlp reporting version, and its version record.
func versioned(t *testing.T, dir, version string, record bool) string {
	t.Helper()
	p := script(t, filepath.Join(dir, "yt-dlp"), "echo "+version)
	if record {
		os.WriteFile(filepath.Join(dir, "yt-dlp.version"), []byte(version+"\n"), 0o644)
	}
	return p
}

func installedVersion(t *testing.T, d Dirs) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(d.Managed, "yt-dlp"))
	return strings.TrimPrefix(strings.TrimSpace(string(data)), "#!/bin/sh\necho ")
}

func TestSeedRefreshesFromNewerBundle(t *testing.T) {
	tests := []struct {
		name                     string
		bundled, managed         string
		recordBundled, recordMgd bool
		want                     string
	}{
		{"newer bundle replaces", "2026.10.26", "2026.08.19", true, true, "2026.10.26"},
		{"older bundle keeps the update", "2026.08.19", "2026.10.26", true, true, "2026.10.26"},
		{"same version keeps", "2026.10.26", "2026.10.26", true, true, "2026.10.26"},
		{"unrecorded managed copy is asked", "2026.10.26", "2026.08.19", true, false, "2026.10.26"},
		{"unknown bundle keeps", "2026.10.26", "2026.08.19", false, true, "2026.08.19"},
	}
	for _, tt := range tests {
		d := layout(t)
		versioned(t, d.Bundled, tt.bundled, tt.recordBundled)
		versioned(t, d.Managed, tt.managed, tt.recordMgd)
		if err := Seed(d); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := installedVersion(t, d); got != tt.want {
			t.Errorf("%s: managed copy is %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestSeedRecordsVersion(t *testing.T) {
	d := layout(t)
	versioned(t, d.Bundled, "2026.10.26", true)
	if err := Seed(d); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(d.Managed, "yt-dlp.version")); strings.TrimSpace(string(data)) != "2026.10.26" {
		t.Errorf("managed version record %q", data)
	}
}

func TestUpdateRecordsVersion(t *testing.T) {
	d := layout(t)
	path := script(t, filepath.Join(d.Managed, "yt-dlp"), `echo "Updated yt-dlp to stable@2026.11.02 from yt-dlp/yt-dlp"`)
	if got := Update(context.Background(), Tool{path, Managed}); !got.OK {
		t.Fatalf("Update = %+v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(d.Managed, "yt-dlp.version")); strings.TrimSpace(string(data)) != "2026.11.02" {
		t.Errorf("managed version record %q", data)
	}
}

func TestSeedReplacesAnUnreadableCopy(t *testing.T) {
	d := layout(t)
	versioned(t, d.Bundled, "2026.10.26", true)
	script(t, filepath.Join(d.Managed, "yt-dlp"), "exit 1") // corrupt: no record, no --version
	if err := Seed(d); err != nil {
		t.Fatal(err)
	}
	if got := installedVersion(t, d); got != "2026.10.26" {
		t.Errorf("managed copy is %q, want the bundled one", got)
	}
}

func TestLoginShellOutputWithABanner(t *testing.T) {
	path := script(t, filepath.Join(t.TempDir(), "yt-dlp"), "")
	shell := script(t, filepath.Join(t.TempDir(), "sh"), `echo "Welcome back!"; echo; echo "`+path+`"`)
	t.Setenv("SHELL", shell)
	if got := lookupInLoginShell("yt-dlp"); got != path {
		t.Errorf("lookupInLoginShell = %q, want %q", got, path)
	}
}

func TestTimeoutsStopTheWholeProcessGroup(t *testing.T) {
	d := layout(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	// Like the bundled yt-dlp, a launcher whose worker is a child process.
	hanging := script(t, filepath.Join(d.Managed, "yt-dlp"), "sleep 30 & echo $! > "+pidFile+"; wait")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	Update(ctx, Tool{hanging, Managed})
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("worker %d outlived the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
