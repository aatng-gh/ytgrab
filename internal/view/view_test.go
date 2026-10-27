package view

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/aatng-gh/ytgrab/internal/job"
	"github.com/aatng-gh/ytgrab/internal/request"
	"github.com/aatng-gh/ytgrab/internal/settings"
	"github.com/aatng-gh/ytgrab/internal/tools"
	"github.com/aatng-gh/ytgrab/internal/ytdlp"
)

// fakeHost queues work for the main thread until the test drains it, and
// records what the app asked of the desktop.
type fakeHost struct {
	mu      sync.Mutex
	queued  []func()
	folder  string
	file    string
	clip    string
	copied  string
	reveals []string
}

func (h *fakeHost) Update(fn func()) {
	h.mu.Lock()
	h.queued = append(h.queued, fn)
	h.mu.Unlock()
}

func (h *fakeHost) drain() {
	for {
		h.mu.Lock()
		q := h.queued
		h.queued = nil
		h.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			fn()
		}
	}
}

func (h *fakeHost) ChooseFolder(string) string { return h.folder }
func (h *fakeHost) ChooseFile(string) string   { return h.file }
func (h *fakeHost) ReadClipboard() string      { return h.clip }
func (h *fakeHost) WriteClipboard(s string)    { h.copied = s }
func (h *fakeHost) Reveal(path string)         { h.reveals = append(h.reveals, path) }

type harness struct {
	t       *testing.T
	app     *App
	host    *fakeHost
	tt      *ui.Tester
	store   *settings.Store
	updates chan tools.Tool

	mu     sync.Mutex
	result ytdlp.UpdateResult // what the fake updater reports
	gate   chan struct{}      // when set, the updater waits for it

	busy bool // set by tests that hold background work open: skip the first settle
}

func (h *harness) setResult(r ytdlp.UpdateResult) {
	h.mu.Lock()
	h.result = r
	h.mu.Unlock()
}

var upToDate = ytdlp.UpdateResult{OK: true, Version: "2026.10.26"}

// managedTools are tools whose yt-dlp is the app's managed copy, backed by a
// script that succeeds after printing a little output.
func managedTools(t *testing.T) tools.Found {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return tools.Found{
		YtDLP:  tools.Tool{Path: write("yt-dlp", `echo "[download] Destination: /out/clip.mp4"`), Source: tools.Managed},
		FFmpeg: tools.Tool{Path: write("ffmpeg", ""), Source: tools.Bundled},
	}
}

// newHarness builds the app and attaches it; opts run first, before the
// launch lookup and update check.
func newHarness(t *testing.T, found tools.Found, s *settings.Settings, opts ...func(*harness)) *harness {
	t.Helper()
	store := settings.Open(t.TempDir())
	if s == nil {
		// Never the real Downloads folder, which macOS guards.
		d := settings.Defaults()
		d.OutputDir = t.TempDir()
		s = &d
	}
	if err := store.Save(*s); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, host: &fakeHost{}, store: store, updates: make(chan tools.Tool, 10), result: upToDate}
	h.app = New(store, tools.Dirs{})
	h.app.seed = func() error { return nil }
	h.app.find = func(string) tools.Found { return found }
	h.app.update = func(_ context.Context, tl tools.Tool) ytdlp.UpdateResult {
		h.updates <- tl
		h.mu.Lock()
		r, gate := h.result, h.gate
		h.mu.Unlock()
		if gate != nil {
			<-gate
		}
		return r
	}
	for _, opt := range opts {
		opt(h)
	}
	h.app.Attach(h.host)
	t.Cleanup(h.app.Close)
	h.tt = ui.NewTester(h.app.View, 520, 900)
	if !h.busy {
		h.settle()
	}
	return h
}

// settle runs background work and queued main-thread updates, then a frame.
func (h *harness) settle() {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !h.app.toolsChecked || h.app.ytdlpUpdate.Running {
		h.host.drain()
		if time.Now().After(deadline) {
			h.t.Fatal("background work never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.host.drain()
	h.tt.Frame()
}

func (h *harness) click(label string) {
	h.t.Helper()
	if err := h.tt.Click(label); err != nil {
		h.t.Fatalf("click %q: %v; texts %q", label, err, h.tt.Texts())
	}
	h.settle()
}

func (h *harness) typeInto(label, text string) {
	h.t.Helper()
	h.click(label)
	h.tt.Type(text)
	h.settle()
}

func (h *harness) has(texts ...string) {
	h.t.Helper()
	for _, s := range texts {
		if !slices.Contains(h.tt.Texts(), s) {
			h.t.Errorf("missing %q in %q", s, h.tt.Texts())
		}
	}
}

func (h *harness) lacks(texts ...string) {
	h.t.Helper()
	for _, s := range texts {
		if slices.Contains(h.tt.Texts(), s) {
			h.t.Errorf("unexpected %q in %q", s, h.tt.Texts())
		}
	}
}

// waitJob waits for the runner to finish and the view to hear of it.
func (h *harness) waitJob() job.State {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.settle()
		if s := h.app.job; s.Status != job.Idle && !s.Status.Running() {
			return s
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("job never finished: view %+v\nrunner %+v", h.app.job, h.app.runner.State())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownload(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.has("Source", "Format", "Sign-in", "Destination", "Download")

	h.click("Download")
	if h.app.job.Status != job.Idle {
		t.Fatalf("Download started without a link: %+v", h.app.job)
	}

	h.typeInto("Link", "ftp://example.com/v")
	h.has("The link must start with http:// or https://.")

	h.host.clip = "https://www.youtube.com/watch?v=abc\n"
	h.click("Paste")
	if h.app.form.URL != "https://www.youtube.com/watch?v=abc" {
		t.Errorf("pasted URL %q", h.app.form.URL)
	}
	h.lacks("The link must start with http:// or https://.")

	h.click("Download")
	s := h.waitJob()
	if s.Status != job.Done || s.File != "/out/clip.mp4" {
		t.Fatalf("job %+v", s)
	}
	h.has("Done", "Reveal in Finder")
	h.click("Reveal in Finder")
	if !slices.Equal(h.host.reveals, []string{"/out/clip.mp4"}) {
		t.Errorf("revealed %q", h.host.reveals)
	}
}

func TestFormatChoices(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.has("Quality", "Format", "1080p", "MP4 (most compatible)", "Include English subtitles", "Embed metadata and cover art")
	h.lacks("M4A (AAC)")

	h.click("Audio only")
	h.has("Quality", "Format", "Embed metadata and cover art")
	h.lacks("Include English subtitles")
	if h.store.Load().Options.Kind != request.Audio {
		t.Error("the Audio choice wasn't saved")
	}

	h.click("M4A (AAC)")
	if err := h.tt.ChooseMenuItem("FLAC (lossless)"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if h.app.form.Options.Audio.Format != request.FLAC {
		t.Errorf("format %q after choosing FLAC", h.app.form.Options.Audio.Format)
	}
	h.has("Lossless formats keep whatever the source provides.")
}

func TestRange(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.lacks("Start", "End")
	h.click("Download only a time range")
	h.has("Start", "End")
	h.typeInto("Start", "2:00")
	h.typeInto("End", "1:00")
	h.has("The start must come before the end.")

	if r := h.app.request(); r.Range == nil || r.Range.Start != "2:00" || r.Range.End != "1:00" {
		t.Errorf("request range %+v", r.Range)
	}
	h.click("Download only a time range")
	if r := h.app.request(); r.Range != nil {
		t.Errorf("range kept after turning it off: %+v", r.Range)
	}
}

func TestSignIn(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.has("Public videos only.")

	h.click("Paste cookies")
	h.has("Paste from clipboard", "Load file…", "nothing pasted yet")
	h.host.clip = ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tvalue"
	h.click("Paste from clipboard")
	h.has("1 cookie")

	h.click("From browser")
	h.has("From", "Chrome", "Profile")
	h.lacks("Load file…")
}

func TestLoadCookieFile(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.click("Paste cookies")

	h.host.file = filepath.Join(t.TempDir(), "cookies.txt")
	os.WriteFile(h.host.file, []byte(".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tv\n.youtube.com\tTRUE\t/\tTRUE\t0\tHSID\tv"), 0o600)
	h.click("Load file…")
	h.has("2 cookies")

	h.host.file = filepath.Join(t.TempDir(), "missing.txt")
	h.click("Load file…")
	h.has("Couldn't read missing.txt.")
}

func TestRunningLocksTheForm(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.app.job = job.State{Status: job.Downloading, Progress: 0.4, Speed: "2.1MiB/s", ETA: "00:12", Title: "Clip"}
	h.settle()
	h.has("Downloading", "2.1MiB/s · ETA 00:12", "Cancel", "Clip")
	h.lacks("Download")

	h.click("Audio only")
	if h.app.form.Options.Kind != request.Video {
		t.Error("the form changed while a job ran")
	}
}

func TestFailureOffersUpdate(t *testing.T) {
	h := newHarness(t, managedTools(t), &settings.Settings{LastYtDLPCheck: time.Now()})
	h.app.job = job.State{Status: job.Failed, Error: "[youtube] abc: Sign in to confirm you're not a bot"}
	h.settle()
	h.has("Failed", "[youtube] abc: Sign in to confirm you're not a bot", "Update yt-dlp")

	h.click("Update yt-dlp")
	select {
	case tl := <-h.updates:
		if tl.Source != tools.Managed {
			t.Errorf("updated %+v", tl)
		}
	default:
		t.Fatal("Update yt-dlp didn't update")
	}
	h.has("yt-dlp is already up to date (2026.10.26).", "[youtube] abc: Sign in to confirm you're not a bot")
}

func TestNoUpdateButtonForOtherInstalls(t *testing.T) {
	found := managedTools(t)
	found.YtDLP.Source = tools.System
	h := newHarness(t, found, nil)
	h.app.job = job.State{Status: job.Failed, Error: "boom"}
	h.settle()
	h.lacks("Update yt-dlp")
	if len(h.updates) != 0 {
		t.Error("a system yt-dlp was updated at launch")
	}
}

func TestLaunchUpdatesWhenDue(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	if len(h.updates) != 1 {
		t.Fatalf("%d updates at launch, want 1", len(h.updates))
	}
	if h.store.Load().LastYtDLPCheck.IsZero() {
		t.Error("the check time wasn't saved")
	}

	recent := newHarness(t, managedTools(t), &settings.Settings{LastYtDLPCheck: time.Now().Add(-time.Hour)})
	if len(recent.updates) != 0 {
		t.Error("updated though checked an hour ago")
	}
}

func TestMissingTools(t *testing.T) {
	h := newHarness(t, tools.Found{}, nil)
	h.has("yt-dlp and FFmpeg not found", "Locate…")

	located := managedTools(t).YtDLP.Path
	h.host.file = located
	var asked string
	h.app.find = func(l string) tools.Found { asked = l; return managedTools(t) }
	h.click("Locate…")
	if asked != located || h.store.Load().YtDLPPath != located {
		t.Errorf("located %q, saved %q", asked, h.store.Load().YtDLPPath)
	}
	h.lacks("yt-dlp and FFmpeg not found")
}

func TestDestination(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.host.folder = "/Volumes/Media"
	h.click("Choose…")
	h.has("/Volumes/Media")
	if h.store.Load().OutputDir != "/Volumes/Media" {
		t.Error("folder not saved")
	}
}

func TestLogDrawer(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.app.job = job.State{Status: job.Failed, Error: "boom", Log: []string{"$ yt-dlp …", "ERROR: boom", "— boom —"}}
	h.settle()
	h.lacks("ERROR: boom")
	h.click("Log")
	h.has("ERROR: boom", "3 lines")
	h.click("Copy")
	if h.host.copied != strings.Join(h.app.job.Log, "\n") {
		t.Errorf("copied %q", h.host.copied)
	}
}

func TestRemembersChoices(t *testing.T) {
	saved := settings.Defaults()
	saved.Options.Kind = request.Audio
	saved.Browser = "firefox"
	saved.OutputDir = "/Volumes/Media"
	h := newHarness(t, managedTools(t), &saved)
	if h.app.form.Options.Kind != request.Audio || h.app.form.Auth.Browser != "firefox" || h.app.form.OutputDir != "/Volumes/Media" {
		t.Errorf("form %+v", h.app.form)
	}
	h.has("M4A (AAC)", "/Volumes/Media")
}

func TestLaunchCheckIsQuiet(t *testing.T) {
	h := newHarness(t, managedTools(t), nil) // checks at launch, finds it current
	for _, text := range h.tt.Texts() {
		if strings.Contains(text, "yt-dlp") {
			t.Errorf("launch check shows %q", text)
		}
	}
}

func TestLaunchShowsAnUpdate(t *testing.T) {
	h := newHarness(t, managedTools(t), nil, func(h *harness) {
		h.result = ytdlp.UpdateResult{OK: true, Updated: true, Version: "2026.11.02"}
	})
	h.has("yt-dlp updated to 2026.11.02.")
}

func TestLaunchShowsAFailedCheck(t *testing.T) {
	h := newHarness(t, managedTools(t), nil, func(h *harness) {
		h.result = ytdlp.UpdateResult{Error: "Unable to obtain version info (<urlopen error [Errno 8] nodename nor servname provided>)"}
	})
	h.has("Couldn't check for yt-dlp updates. Check your internet connection.")
	if !h.store.Load().LastYtDLPCheck.IsZero() {
		t.Error("a failed check was recorded as done")
	}
}

func TestOtherUpdateErrorsAreShown(t *testing.T) {
	h := newHarness(t, managedTools(t), nil, func(h *harness) {
		h.result = ytdlp.UpdateResult{Error: "the update timed out."}
	})
	h.has("Couldn't update yt-dlp: the update timed out.")
}

func TestMenuUpdateShowsTheResult(t *testing.T) {
	h := newHarness(t, managedTools(t), &settings.Settings{LastYtDLPCheck: time.Now()})
	h.app.UpdateYtDLP()
	h.settle()
	h.has("yt-dlp is already up to date (2026.10.26).")
}

func TestUpdateMessageClearsOnDownload(t *testing.T) {
	h := newHarness(t, managedTools(t), nil, func(h *harness) {
		h.result = ytdlp.UpdateResult{OK: true, Updated: true, Version: "2026.11.02"}
	})
	h.app.form.URL = "https://example.com/v"
	h.settle()
	h.click("Download")
	h.waitJob()
	h.lacks("yt-dlp updated to 2026.11.02.")
}

// flakyTools fail the first download and succeed after.
func flakyTools(t *testing.T) tools.Found {
	found := managedTools(t)
	marker := filepath.Join(t.TempDir(), "ran")
	os.WriteFile(found.YtDLP.Path, []byte(`#!/bin/sh
if [ ! -e `+marker+` ]; then touch `+marker+`; echo "ERROR: [youtube] abc: nsig extraction failed" >&2; exit 1; fi
echo "[download] Destination: /out/clip.mp4"
`), 0o755)
	return found
}

func TestUpdateAfterFailureRetries(t *testing.T) {
	h := newHarness(t, flakyTools(t), &settings.Settings{LastYtDLPCheck: time.Now()})
	h.app.form.URL = "https://example.com/v"
	h.app.form.OutputDir = t.TempDir()
	h.settle()
	h.click("Download")
	if s := h.waitJob(); s.Status != job.Failed {
		t.Fatalf("first run %+v", s)
	}

	h.setResult(ytdlp.UpdateResult{OK: true, Updated: true, Version: "2026.11.02"})
	h.click("Update yt-dlp")
	if s := h.waitJob(); s.Status != job.Done {
		t.Fatalf("retried run %+v", s)
	}
	h.has("yt-dlp updated to 2026.11.02. Tried the download again.")
}

func TestNoRetryWhenAlreadyCurrent(t *testing.T) {
	h := newHarness(t, flakyTools(t), &settings.Settings{LastYtDLPCheck: time.Now()})
	h.app.form.URL = "https://example.com/v"
	h.app.form.OutputDir = t.TempDir()
	h.settle()
	h.click("Download")
	h.waitJob()

	h.click("Update yt-dlp")
	if h.app.job.Status != job.Failed {
		t.Errorf("retried though nothing was updated: %+v", h.app.job)
	}
	h.has("[youtube] abc: nsig extraction failed", "yt-dlp is already up to date (2026.10.26).")
}

func TestOutsideUpdateIsExplained(t *testing.T) {
	found := managedTools(t)
	found.YtDLP.Source = tools.System
	h := newHarness(t, found, nil)
	h.app.job = job.State{Status: job.Failed, Error: "boom"}
	h.app.UpdateYtDLP()
	h.settle()
	h.has("boom", "This yt-dlp was installed outside YTGrab. Update it where you installed it, such as with brew upgrade yt-dlp.")
	if len(h.updates) != 0 {
		t.Error("ran the updater on a system yt-dlp")
	}
}

func TestDownloadDuringAnUpdate(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, managedTools(t), &settings.Settings{LastYtDLPCheck: time.Now()}, func(h *harness) { h.gate = gate })
	defer close(gate)
	h.app.form.URL = "https://example.com/v"
	h.app.form.OutputDir = t.TempDir()
	h.app.UpdateYtDLP()
	h.tt.Frame()
	h.has("Checking for yt-dlp updates…")
	if err := h.tt.Click("Download"); err != nil {
		t.Fatal(err)
	}
	h.host.drain()
	if h.app.job.Status == job.Idle {
		t.Error("Download did nothing while yt-dlp was updating")
	}
}

func TestCookieNoticeClearsOnEdit(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.click("Paste cookies")
	h.host.file = filepath.Join(t.TempDir(), "missing.txt")
	h.click("Load file…")
	h.has("Couldn't read missing.txt.")
	h.typeInto("Jar", ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tv")
	h.lacks("Couldn't read missing.txt.")

	h.click("Load file…")
	h.has("Couldn't read missing.txt.")
	h.click("Clear")
	h.lacks("Couldn't read missing.txt.")
}

func TestJobChangesAreBatched(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	for i := 0; i < 500; i++ {
		h.app.jobChanged()
	}
	h.host.mu.Lock()
	queued := len(h.host.queued)
	h.host.mu.Unlock()
	if queued != 1 {
		t.Errorf("%d updates queued for 500 changes, want 1", queued)
	}
	h.settle()
	h.app.jobChanged()
	h.host.mu.Lock()
	queued = len(h.host.queued)
	h.host.mu.Unlock()
	if queued != 1 {
		t.Errorf("a change after the batch ran queued %d updates, want 1", queued)
	}
}

func TestMenuUpdateDuringTheLaunchCheck(t *testing.T) {
	gate := make(chan struct{})
	h := newHarness(t, managedTools(t), nil, func(h *harness) { h.gate, h.busy = gate, true })
	for deadline := time.Now().Add(3 * time.Second); !h.app.ytdlpUpdate.Running; time.Sleep(5 * time.Millisecond) {
		h.host.drain()
		if time.Now().After(deadline) {
			t.Fatal("no launch check running")
		}
	}
	h.app.UpdateYtDLP()
	close(gate)
	h.settle()
	h.has("yt-dlp is already up to date (2026.10.26).")
}

func TestMenuUpdateBeforeToolsAreFound(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	h.app.tools, h.app.toolsChecked = tools.Found{}, false // as before the lookup answers
	h.app.UpdateYtDLP()
	h.tt.Frame()
	for _, text := range h.tt.Texts() {
		if strings.Contains(text, "outside YTGrab") {
			t.Errorf("shows %q before the lookup finished", text)
		}
	}
}

func TestMenuUpdateWithoutYtDLP(t *testing.T) {
	h := newHarness(t, tools.Found{}, nil)
	h.app.UpdateYtDLP()
	h.settle()
	h.has("yt-dlp wasn't found. Use Locate… to choose it.")
	h.lacks("This yt-dlp was installed outside YTGrab. Update it where you installed it, such as with brew upgrade yt-dlp.")
}

func TestAnOlderLookupDoesNotWin(t *testing.T) {
	slow := make(chan struct{})
	located := managedTools(t)
	located.YtDLP.Source = tools.Located
	h := newHarness(t, tools.Found{}, nil, func(h *harness) {
		h.busy = true
		h.app.find = func(l string) tools.Found {
			if l == "" {
				<-slow // the launch lookup, stuck in a slow login shell
				return tools.Found{}
			}
			return located
		}
	})
	h.host.file = located.YtDLP.Path
	h.app.locateYtDLP()
	for deadline := time.Now().Add(3 * time.Second); h.app.tools != located; time.Sleep(5 * time.Millisecond) {
		h.host.drain()
		if time.Now().After(deadline) {
			t.Fatal("the located yt-dlp never arrived")
		}
	}
	close(slow)
	time.Sleep(50 * time.Millisecond)
	h.host.drain()
	if h.app.tools != located {
		t.Errorf("the older launch lookup replaced the located yt-dlp: %+v", h.app.tools)
	}
}

func TestCloseCancelsAnUpdate(t *testing.T) {
	cancelled := make(chan struct{})
	h := newHarness(t, managedTools(t), &settings.Settings{LastYtDLPCheck: time.Now()}, func(h *harness) {
		h.app.update = func(ctx context.Context, _ tools.Tool) ytdlp.UpdateResult {
			<-ctx.Done()
			close(cancelled)
			return ytdlp.UpdateResult{Error: "cancelled"}
		}
	})
	h.app.UpdateYtDLP()
	h.app.Close()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Close left the yt-dlp update running")
	}
}

// TestLayoutHoldsStill checks that switching modes doesn't move the controls
// sideways: the form's label column is as wide as its widest shown label.
func TestLayoutHoldsStill(t *testing.T) {
	h := newHarness(t, managedTools(t), nil)
	x := func(label string) float32 {
		t.Helper()
		r, ok := h.tt.Find(label)
		if !ok {
			t.Fatalf("%q not shown; texts %q", label, h.tt.Texts())
		}
		return r.X
	}
	want := x("Video") // left-aligned beside the labels, and always shown
	check := func(state string) {
		t.Helper()
		if got := x("Video"); got != want {
			t.Errorf("%s: the form moved by %v", state, got-want)
		}
	}
	h.click("Audio only")
	check("audio")
	h.click("Paste cookies")
	check("pasted cookies")
	h.click("From browser")
	check("from browser")
	h.click("Download only a time range")
	check("time range")
	h.click("Video")
	check("back to video")
}
