package job

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/aatng-gh/ytgrab/internal/request"
	"github.com/aatng-gh/ytgrab/internal/tools"
)

const cookie = ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tvalue"

// fake writes a yt-dlp stand-in and returns tools that use it.
func fake(t *testing.T, body string) tools.Found {
	t.Helper()
	dir := t.TempDir()
	ytdlp := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(ytdlp, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return tools.Found{
		YtDLP:  tools.Tool{Path: ytdlp, Source: tools.Bundled},
		FFmpeg: tools.Tool{Path: ffmpeg, Source: tools.Bundled},
	}
}

func req(t *testing.T) request.Request {
	return request.Request{
		URL:       "https://example.com/watch?v=abc",
		Options:   request.DefaultOptions(),
		OutputDir: t.TempDir(),
	}
}

// runner returns a Runner whose changes are counted, and fails the test if
// it is left running.
func runner(t *testing.T) (*Runner, *atomic.Int64) {
	t.Helper()
	changes := new(atomic.Int64)
	r := NewRunner(func() { changes.Add(1) })
	r.killAfter = 300 * time.Millisecond
	t.Cleanup(r.Close)
	return r, changes
}

// wait polls until cond holds for the runner's state.
func wait(t *testing.T, r *Runner, within time.Duration, cond func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		s := r.State()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out; state %+v", s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func finished(s State) bool { return !s.Status.Running() && s.Status != Idle }

func TestSuccessfulRun(t *testing.T) {
	r, changes := runner(t)
	found := fake(t, `
echo "[youtube] abc: Downloading webpage"
echo "@@PROG@@ 50.0%|1.00MiB/s|00:05|My Video"
echo "[download] Destination: /out/My Video [abc].f137.mp4"
echo '[Merger] Merging formats into "/out/My Video [abc].mp4"'
exit 0`)
	if s := r.State(); s.Status != Idle {
		t.Fatalf("new runner: %+v", s)
	}
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, finished)

	if s.Status != Done || s.Progress != 1 || s.Title != "My Video" || s.File != "/out/My Video [abc].mp4" || s.Error != "" {
		t.Errorf("final state %+v", s)
	}
	if len(s.Log) < 4 || !strings.HasPrefix(s.Log[0], "$ "+found.YtDLP.Path+" ") {
		t.Fatalf("log %q", s.Log)
	}
	if !strings.Contains(s.Log[0], "--ffmpeg-location "+filepath.Dir(found.FFmpeg.Path)) {
		t.Errorf("command line lacks the ffmpeg location: %s", s.Log[0])
	}
	for _, line := range s.Log[1:] { // the command line holds the template itself
		if strings.Contains(line, "@@PROG@@") {
			t.Errorf("progress line in log: %q", line)
		}
	}
	if changes.Load() == 0 {
		t.Error("onChange never called")
	}
}

func TestPhases(t *testing.T) {
	r, _ := runner(t)
	gate := filepath.Join(t.TempDir(), "gate")
	found := fake(t, `
echo "@@PROG@@ 10.0%|1MiB/s|00:09|T"
echo "[ExtractAudio] Destination: /out/T.m4a"
while [ ! -e `+gate+` ]; do sleep 0.02; done`)
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, func(s State) bool { return s.Status == Processing })
	if s.Phase != "extracting" || s.Progress != -1 || s.Speed != "" || s.ETA != "" || s.File != "/out/T.m4a" {
		t.Errorf("processing state %+v", s)
	}
	os.WriteFile(gate, nil, 0o600)
	wait(t, r, 5*time.Second, finished)
}

func TestDownloadingState(t *testing.T) {
	r, _ := runner(t)
	gate := filepath.Join(t.TempDir(), "gate")
	found := fake(t, `
echo "@@PROG@@ 25.0%|2.00MiB/s|00:30|Title"
while [ ! -e `+gate+` ]; do sleep 0.02; done`)
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, func(s State) bool { return s.Status == Downloading })
	if s.Progress != 0.25 || s.Speed != "2.00MiB/s" || s.ETA != "00:30" {
		t.Errorf("downloading state %+v", s)
	}
	if err := r.Start(req(t), found); err == nil {
		t.Error("a second Start succeeded while running")
	}
	os.WriteFile(gate, nil, 0o600)
	wait(t, r, 5*time.Second, finished)
}

func TestFailure(t *testing.T) {
	r, _ := runner(t)
	if err := r.Start(req(t), fake(t, `echo "ERROR: [youtube] abc: Video unavailable" >&2; exit 1`)); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, finished)
	if s.Status != Failed || s.Error != "[youtube] abc: Video unavailable" || s.Progress != 0 {
		t.Errorf("state %+v", s)
	}

	if err := r.Start(req(t), fake(t, "exit 2")); err != nil {
		t.Fatal(err)
	}
	s = wait(t, r, 5*time.Second, finished)
	if s.Status != Failed || s.Error != "yt-dlp exited with code 2." {
		t.Errorf("state %+v", s)
	}
}

func TestStartRejects(t *testing.T) {
	r, _ := runner(t)
	found := fake(t, "exit 0")

	bad := req(t)
	bad.URL = ""
	if err := r.Start(bad, found); err == nil {
		t.Error("started an invalid request")
	}
	if err := r.Start(req(t), tools.Found{FFmpeg: found.FFmpeg}); err == nil {
		t.Error("started without yt-dlp")
	}
	if s := r.State(); s.Status != Idle {
		t.Errorf("rejected starts changed the state: %+v", s)
	}
}

func TestCancelStopsTheProcessGroup(t *testing.T) {
	r, _ := runner(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	found := fake(t, `
sleep 30 &
echo $! > `+pidFile+`
echo "@@PROG@@ 1.0%|1MiB/s|01:00|T"
wait`)
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	wait(t, r, 5*time.Second, func(s State) bool { return s.Status == Downloading })

	r.Cancel()
	if s := r.State(); s.Status != Cancelling && s.Status != Cancelled {
		t.Errorf("after Cancel: %+v", s)
	}
	s := wait(t, r, 5*time.Second, finished)
	if s.Status != Cancelled {
		t.Errorf("final state %+v", s)
	}
	assertDead(t, pidFile)
}

func TestCancelKillsWhatIgnoresTERM(t *testing.T) {
	r, _ := runner(t)
	found := fake(t, `
trap '' TERM
echo "@@PROG@@ 1.0%|1MiB/s|01:00|T"
while :; do sleep 0.05; done`)
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	wait(t, r, 5*time.Second, func(s State) bool { return s.Status == Downloading })
	r.Cancel()
	if s := wait(t, r, 5*time.Second, finished); s.Status != Cancelled {
		t.Errorf("final state %+v", s)
	}
}

func TestGrandchildHoldingThePipe(t *testing.T) {
	r, _ := runner(t)
	// Like ffmpeg, the background child inherits stdout and outlives yt-dlp.
	pidFile := filepath.Join(t.TempDir(), "pid")
	found := fake(t, `sleep 30 & echo $! > `+pidFile+`; echo started; exit 0`)
	start := time.Now()
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, finished)
	if s.Status != Done {
		t.Errorf("final state %+v", s)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %v; waited for the grandchild", time.Since(start))
	}
	assertDead(t, pidFile)
}

func TestPastedCookies(t *testing.T) {
	r, _ := runner(t)
	seen := filepath.Join(t.TempDir(), "seen")
	found := fake(t, `
while [ $# -gt 0 ]; do
  if [ "$1" = --cookies ]; then echo "$2" > `+seen+`; cat "$2" >> `+seen+`; fi
  shift
done`)
	q := req(t)
	q.Auth = request.Auth{Mode: request.PastedCookies, Cookies: cookie}
	if err := r.Start(q, found); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, finished)

	data, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("yt-dlp got no --cookies: %v", err)
	}
	jar, contents, _ := strings.Cut(string(data), "\n")
	if !strings.Contains(contents, "SID\tvalue") {
		t.Errorf("jar held %q", contents)
	}
	if _, err := os.Stat(jar); !os.IsNotExist(err) {
		t.Errorf("jar %s still exists after the job", jar)
	}
	if strings.Contains(s.Log[0], jar) || !strings.Contains(s.Log[0], "<pasted cookies>") {
		t.Errorf("command line exposes the jar: %s", s.Log[0])
	}
}

func TestCloseStopsAndCleansUp(t *testing.T) {
	r := NewRunner(func() {})
	r.killAfter = 300 * time.Millisecond
	seen := filepath.Join(t.TempDir(), "jar")
	found := fake(t, `
while [ $# -gt 0 ]; do [ "$1" = --cookies ] && echo "$2" > `+seen+`; shift; done
echo "@@PROG@@ 1.0%|1MiB/s|01:00|T"
trap '' TERM
while :; do sleep 0.05; done`)
	q := req(t)
	q.Auth = request.Auth{Mode: request.PastedCookies, Cookies: cookie}
	if err := r.Start(q, found); err != nil {
		t.Fatal(err)
	}
	wait(t, r, 5*time.Second, func(s State) bool { return s.Status == Downloading })

	start := time.Now()
	r.Close()
	if time.Since(start) > 3*time.Second {
		t.Errorf("Close took %v", time.Since(start))
	}
	if r.State().Status.Running() {
		t.Errorf("still running after Close: %+v", r.State())
	}
	jar, _ := os.ReadFile(seen)
	if _, err := os.Stat(strings.TrimSpace(string(jar))); !os.IsNotExist(err) {
		t.Errorf("jar %s survived Close", jar)
	}
	r.Close()
}

func TestLogIsCapped(t *testing.T) {
	r, _ := runner(t)
	found := fake(t, `i=0; while [ $i -lt 2100 ]; do echo "line $i"; i=$((i+1)); done`)
	if err := r.Start(req(t), found); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 10*time.Second, finished)
	if len(s.Log) != maxLogLines {
		t.Fatalf("log has %d lines", len(s.Log))
	}
	if last := s.Log[len(s.Log)-2]; last != "line 2099" {
		t.Errorf("second-last log line %q", last)
	}
}

func TestStateIsACopy(t *testing.T) {
	r, _ := runner(t)
	if err := r.Start(req(t), fake(t, "echo one")); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, finished)
	s.Log[0] = "changed"
	if r.State().Log[0] == "changed" {
		t.Error("State shares its log with the runner")
	}
}

func assertDead(t *testing.T, pidFile string) {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Errorf("grandchild %d survived", pid)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCrashIsAFailure(t *testing.T) {
	r, _ := runner(t)
	if err := r.Start(req(t), fake(t, `echo "@@PROG@@ 5.0%|1MiB/s|00:10|T"; kill -ABRT $$`)); err != nil {
		t.Fatal(err)
	}
	s := wait(t, r, 5*time.Second, finished)
	if s.Status != Failed || !strings.Contains(s.Error, "stopped unexpectedly") {
		t.Errorf("state %+v", s)
	}
}

func TestCancelAfterExitKeepsTheOutcome(t *testing.T) {
	r, _ := runner(t)
	// The background child holds the pipes, so the job winds up for a while
	// after yt-dlp itself has exited successfully.
	if err := r.Start(req(t), fake(t, `sleep 30 & echo "@@PROG@@ 100.0%|1MiB/s|00:00|T"; exit 0`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		reaped := r.proc != nil && r.proc.reaped
		r.mu.Unlock()
		if reaped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("yt-dlp never exited")
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.Cancel()
	if s := wait(t, r, 5*time.Second, finished); s.Status != Done {
		t.Errorf("a finished download became %s", s.Status)
	}
}

func TestMissingOutputFolder(t *testing.T) {
	r, _ := runner(t)
	q := req(t)
	q.OutputDir = filepath.Join(t.TempDir(), "ejected-drive", "Videos")
	if err := r.Start(q, fake(t, "exit 0")); err != nil {
		t.Fatal(err)
	}
	if s := wait(t, r, 5*time.Second, finished); s.Status != Done {
		t.Errorf("state %+v", s)
	}
}
