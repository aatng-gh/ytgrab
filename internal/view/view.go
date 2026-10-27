// Package view is the window: it draws app state with MyGo's ui package and
// turns clicks into calls on the other packages. It holds no download logic.
//
// Threading: View and every action run on the main thread. Work that can be
// slow (finding tools, updating yt-dlp) runs on a goroutine and writes its
// result back through Host.Update.
package view

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/aatng-gh/ytgrab/internal/cookies"
	"github.com/aatng-gh/ytgrab/internal/job"
	"github.com/aatng-gh/ytgrab/internal/request"
	"github.com/aatng-gh/ytgrab/internal/settings"
	"github.com/aatng-gh/ytgrab/internal/tools"
	"github.com/aatng-gh/ytgrab/internal/ytdlp"
)

// Host is what the view needs from the desktop. WindowHost implements it for
// a MyGo window; tests use a fake.
type Host interface {
	// Update runs fn on the main thread and redraws.
	Update(fn func())
	// ChooseFolder and ChooseFile return "" when cancelled.
	ChooseFolder(start string) string
	ChooseFile(title string) string
	ReadClipboard() string
	WriteClipboard(text string)
	Reveal(path string)
}

// App is the window's state. Fields are only touched on the main thread.
type App struct {
	host   Host
	store  *settings.Store
	runner *job.Runner

	// Tool operations, replaced in tests.
	seed   func() error
	find   func(located string) tools.Found
	update func(context.Context, tools.Tool) ytdlp.UpdateResult

	// settings as last saved; persist writes the form's remembered parts
	// back when they differ.
	settings settings.Settings
	// form is what the controls edit.
	form request.Request
	// rangeOn shows the range fields; request() builds form.Range from them.
	rangeOn              bool
	rangeStart, rangeEnd string

	// job is the runner's state as of its last change.
	job job.State

	tools        tools.Found
	toolsChecked bool // false until the latest lookup answers
	// lookups counts tool lookups started; only the latest one's answer is
	// applied, so a slow earlier lookup can't undo a Locate….
	lookups     int
	ytdlpUpdate ytdlpUpdate

	// ctx ends when the app closes, stopping background work with it.
	ctx    context.Context
	cancel context.CancelFunc

	logOpen bool
	log     ui.ListState
	// cookieNotice explains a cookie file that couldn't be read. It shows
	// until the cookie text changes from cookieNoticeFor.
	cookieNotice, cookieNoticeFor string

	// jobPending is set while a copy of the runner's state is queued for the
	// main thread, so a burst of output lines queues one copy, not hundreds.
	jobPending atomic.Bool
}

// ytdlpUpdate is the state of the managed yt-dlp's current or last update.
type ytdlpUpdate struct {
	Running bool
	// Done is set once Result holds an outcome.
	Done   bool
	Result ytdlp.UpdateResult
	// Manual is set when the user asked, from the menu or the footer, so the
	// outcome is shown even when nothing changed. The daily check at launch
	// only speaks up when it updated or failed.
	Manual bool
	// Retry is set when the update was asked for after a failed download,
	// which starts again if a newer yt-dlp arrives; Retried once it has.
	Retry, Retried bool
	// Outside is set when the user asked to update a yt-dlp the app doesn't
	// manage; Missing when there is none.
	Outside, Missing bool
}

// updateTone colors the update line.
type updateTone int

const (
	toneMuted updateTone = iota
	toneSuccess
	toneWarning
)

// line is what the footer shows about the update, or "" for nothing.
func (u ytdlpUpdate) line() (string, updateTone) {
	switch {
	case u.Missing:
		return "yt-dlp wasn't found. Use Locate… to choose it.", toneWarning
	case u.Outside:
		return "This yt-dlp was installed outside YTGrab. Update it where you installed it, such as with brew upgrade yt-dlp.", toneMuted
	case u.Running:
		if u.Manual {
			return "Checking for yt-dlp updates…", toneMuted
		}
		return "", toneMuted
	case !u.Done:
		return "", toneMuted
	case !u.Result.OK:
		// yt-dlp reports an unreachable GitHub as a Python URL error.
		if strings.Contains(u.Result.Error, "Unable to obtain version info") || strings.Contains(u.Result.Error, "urlopen error") {
			return "Couldn't check for yt-dlp updates. Check your internet connection.", toneWarning
		}
		return "Couldn't update yt-dlp: " + u.Result.Error, toneWarning
	case u.Result.Updated:
		msg := "yt-dlp updated to " + u.Result.Version + "."
		if u.Retried {
			msg += " Tried the download again."
		}
		return msg, toneSuccess
	case u.Manual:
		if u.Result.Version == "" {
			return "yt-dlp is already up to date.", toneMuted
		}
		return "yt-dlp is already up to date (" + u.Result.Version + ").", toneMuted
	}
	return "", toneMuted
}

// New loads settings and creates the runner. Nothing is shown or looked up
// until Attach.
func New(store *settings.Store, dirs tools.Dirs) *App {
	a := &App{
		store:  store,
		seed:   func() error { return tools.Seed(dirs) },
		find:   func(located string) tools.Found { return tools.Find(dirs, located) },
		update: tools.Update,
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.runner = job.NewRunner(a.jobChanged)
	a.job = a.runner.State()
	a.settings = store.Load()
	a.form = request.Request{
		Options:   a.settings.Options,
		Auth:      request.Auth{Mode: request.NoAuth, Browser: a.settings.Browser},
		OutputDir: a.settings.OutputDir,
	}
	a.log.FollowEnd = true
	return a
}

// Attach connects the app to its window and starts the background lookup.
func (a *App) Attach(h Host) {
	a.host = h
	a.findTools()
}

// Close stops any job, removing its cookie jar, and any yt-dlp update. Called
// when the app quits.
func (a *App) Close() {
	a.cancel()
	a.runner.Close()
}

func (a *App) running() bool { return a.job.Status.Running() }

// request is the form as a Request, with the range applied.
func (a *App) request() request.Request {
	r := a.form
	r.Range = nil
	if a.rangeOn {
		r.Range = &request.Range{Start: a.rangeStart, End: a.rangeEnd}
	}
	return r
}

// problems returns the request's problems by field.
func (a *App) problems() map[request.Field]string {
	out := map[request.Field]string{}
	for _, p := range a.request().Validate() {
		out[p.Field] = p.Message
	}
	return out
}

// View builds the window: banner, the four sections, footer, log drawer.
func (a *App) View(c *ui.Context) {
	if a.cookieNotice != "" && a.form.Auth.Cookies != a.cookieNoticeFor {
		a.cookieNotice = ""
	}
	problems := a.problems()
	ui.Column(c).Fill().Children(func() {
		a.missingToolsBanner(c)
		ui.Scroll(c).Grow(1).Padding(16, 20).Children(func() {
			ui.Form(c, func() {
				a.sourceSection(c, problems)
				a.formatSection(c)
				a.signInSection(c, problems)
				a.destinationSection(c)
			})
		})
		ui.Divider(c)
		a.footer(c, len(problems) == 0 && a.tools.Ready())
		if a.logOpen {
			ui.Divider(c)
			a.logDrawer(c)
		}
	})
	a.persist()
}

// --- Sections ---

func (a *App) missingToolsBanner(c *ui.Context) {
	if !a.toolsChecked || a.tools.Ready() {
		return
	}
	t := c.Theme()
	title := "FFmpeg not found"
	switch {
	case a.tools.YtDLP.Source == tools.Missing && a.tools.FFmpeg.Source == tools.Missing:
		title = "yt-dlp and FFmpeg not found"
	case a.tools.YtDLP.Source == tools.Missing:
		title = "yt-dlp not found"
	}
	ui.Row(c).Gap(10).Padding(10, 16).AlignItems(ui.Center).Background(t.Warning.Alpha(0.15)).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).Bold()
			ui.Text(c, "This copy of YTGrab is missing its bundled tools. Locate yt-dlp, or install both with Homebrew.").
				FontSize(12).TextColor(t.TextMuted)
		})
		if a.tools.YtDLP.Source == tools.Missing && ui.Button(c, "Locate…").Clicked() {
			a.locateYtDLP()
		}
	})
}

func (a *App) sourceSection(c *ui.Context, problems map[request.Field]string) {
	ui.Fieldset(c, "Source", func() {
		urlError := ""
		if strings.TrimSpace(a.form.URL) != "" {
			urlError = problems[request.FieldURL]
		}
		ui.Field(c, "Link", func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.TextInput(c, &a.form.URL).Placeholder("https://www.youtube.com/watch?v=…").Grow(1)
				if ui.Button(c, "Paste").Clicked() {
					a.form.URL = strings.TrimSpace(a.host.ReadClipboard())
				}
			})
		}).Error(urlError)
		ui.Field(c, "Options", func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Checkbox(c, &a.form.Options.Playlist, "Download the whole playlist if the link is one")
				ui.Checkbox(c, &a.rangeOn, "Download only a time range")
			})
		})
		if a.rangeOn {
			ui.Field(c, "Start", func() { ui.TextInput(c, &a.rangeStart).Placeholder("0:00").Width(120) })
			ui.Field(c, "End", func() { ui.TextInput(c, &a.rangeEnd).Placeholder("to the end").Width(120) }).
				Description("h:mm:ss or m:ss. Leave one blank for the start or the end.").
				Error(problems[request.FieldRange])
		}
	}).Disabled(a.running())
}

func (a *App) formatSection(c *ui.Context) {
	opts := &a.form.Options
	ui.Fieldset(c, "Format", func() {
		ui.Field(c, "Type", func() {
			ui.RadioGroup(c, func() {
				ui.Radio(c, &opts.Kind, request.Video, "Video")
				ui.Radio(c, &opts.Kind, request.Audio, "Audio only")
			}).Row().Gap(16)
		})
		if opts.Kind == request.Video {
			// Labels that come and go are no wider than the ones always shown,
			// so the form's label column, and every control, holds still.
			ui.Field(c, "Format", func() { choice(c, &opts.Video.Container, request.Containers, containerLabel) })
			ui.Field(c, "Quality", func() { choice(c, &opts.Video.MaxHeight, request.Heights, heightLabel) })
		} else {
			ui.Field(c, "Format", func() { choice(c, &opts.Audio.Format, request.AudioFormats, formatLabel) })
			bitrate := ui.Field(c, "Quality", func() {
				choice(c, &opts.Audio.Bitrate, request.Bitrates, bitrateLabel).Disabled(opts.Audio.Format.Lossless())
			})
			if opts.Audio.Format.Lossless() {
				bitrate.Description("Lossless formats keep whatever the source provides.")
			}
		}
		ui.Field(c, "Extras", func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Checkbox(c, &opts.EmbedMetadata, "Embed metadata and cover art")
				if opts.Kind == request.Video {
					ui.Checkbox(c, &opts.Subtitles, "Include English subtitles")
				}
			})
		})
	}).Disabled(a.running())
}

func (a *App) signInSection(c *ui.Context, problems map[request.Field]string) {
	auth := &a.form.Auth
	ui.Fieldset(c, "Sign-in", func() {
		ui.Field(c, "Cookies", func() {
			ui.RadioGroup(c, func() {
				ui.Radio(c, &auth.Mode, request.NoAuth, "None")
				ui.Radio(c, &auth.Mode, request.PastedCookies, "Paste cookies")
				ui.Radio(c, &auth.Mode, request.FromBrowser, "From browser")
			}).Row().Gap(16)
		}).Description(map[request.AuthMode]string{
			request.NoAuth:        "Public videos only.",
			request.PastedCookies: "Netscape cookies.txt only. Kept in a private temporary file for the download, then deleted.",
			request.FromBrowser:   "Chromium browsers must be quit first, and macOS will ask for Keychain access.",
		}[auth.Mode])

		switch auth.Mode {
		case request.PastedCookies:
			ui.Field(c, "Jar", func() {
				ui.Column(c).Gap(6).Children(func() {
					ui.TextArea(c, &auth.Cookies).Placeholder("Paste Netscape cookies.txt contents").Font("monospace").FontSize(11).Height(90)
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						if ui.Button(c, "Paste from clipboard").Clicked() {
							auth.Cookies = a.host.ReadClipboard()
						}
						if ui.Button(c, "Load file…").Clicked() {
							a.loadCookieFile()
						}
						if ui.Button(c, "Clear").Disabled(auth.Cookies == "").Clicked() {
							auth.Cookies = ""
						}
						ui.Spacer(c)
						ui.Text(c, cookieSummary(auth.Cookies)).FontSize(12).TextColor(c.Theme().TextMuted)
					})
				})
			}).Error(a.cookieError(problems))
		case request.FromBrowser:
			ui.Field(c, "From", func() { choice(c, &auth.Browser, request.Browsers, browserLabel) })
			ui.Field(c, "Profile", func() { ui.TextInput(c, &auth.Profile).Placeholder("Optional").Width(200) })
		}
	}).Disabled(a.running())
}

func (a *App) cookieError(problems map[request.Field]string) string {
	if a.cookieNotice != "" {
		return a.cookieNotice
	}
	if a.form.Auth.Cookies != "" {
		return problems[request.FieldCookies]
	}
	return ""
}

func (a *App) destinationSection(c *ui.Context) {
	ui.Fieldset(c, "Destination", func() {
		ui.Field(c, "Folder", func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.form.OutputDir).SingleLine().Grow(1).Basis(0).Tooltip(a.form.OutputDir)
				if ui.Button(c, "Choose…").Clicked() {
					a.chooseFolder()
				}
			})
		})
	}).Disabled(a.running())
}

// footer shows the job's status and the actions on it; ready enables Download.
func (a *App) footer(c *ui.Context, ready bool) {
	t := c.Theme()
	s := a.job
	ui.Column(c).Gap(8).Padding(12, 16).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			status := ui.Text(c, statusText(s)).Bold()
			if s.Status == job.Failed {
				status.TextColor(t.Danger)
			}
			ui.Spacer(c)
			if speed := speedText(s); speed != "" {
				ui.Text(c, speed).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if a.running() {
			ui.Progress(c, s.Progress)
		}
		switch {
		case s.Status == job.Failed && s.Error != "":
			ui.Text(c, s.Error).FontSize(12).TextColor(t.TextMuted).Selectable()
		case a.running() && s.Title != "":
			ui.Text(c, s.Title).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		if msg, tone := a.ytdlpUpdate.line(); msg != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if a.ytdlpUpdate.Running {
					ui.Spinner(c).Size(12, 12)
				}
				color := map[updateTone]ui.Color{toneMuted: t.TextMuted, toneSuccess: t.Success, toneWarning: t.Warning}[tone]
				line := ui.Text(c, msg).FontSize(12).TextColor(color).Selectable()
				if a.ytdlpUpdate.Done && !a.ytdlpUpdate.Result.OK {
					line.Tooltip(a.ytdlpUpdate.Result.Error)
				}
			})
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Toggle(c, &a.logOpen, "Log")
			if s.File != "" && !a.running() && ui.Button(c, "Reveal in Finder").Clicked() {
				a.host.Reveal(s.File)
			}
			if s.Status == job.Failed && a.tools.YtDLP.Updatable() {
				if ui.Button(c, "Update yt-dlp").Disabled(a.ytdlpUpdate.Running).
					Tooltip("Get the latest yt-dlp, and try the download again if there's a newer one").Clicked() {
					a.UpdateYtDLP()
				}
			}
			ui.Spacer(c)
			if a.running() {
				if ui.Button(c, "Cancel").Disabled(s.Status == job.Cancelling).Clicked() {
					a.runner.Cancel()
				}
			} else if ui.PrimaryButton(c, "Download").Disabled(!ready).Clicked() {
				a.startDownload()
			}
		})
	})
}

func (a *App) logDrawer(c *ui.Context) {
	t := c.Theme()
	lines := a.job.Log
	ui.Column(c).Height(220).Children(func() {
		ui.List(c, &a.log, len(lines), func(i int) {
			ui.Text(c, lines[i]).Font("monospace").FontSize(11).Selectable()
		}).Grow(1).Padding(6, 16)
		ui.Row(c).Gap(6).Padding(6, 16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, plural(len(lines), "line")).FontSize(12).TextColor(t.TextMuted)
			ui.Spacer(c)
			if ui.Button(c, "Copy").Disabled(len(lines) == 0).Clicked() {
				a.host.WriteClipboard(strings.Join(lines, "\n"))
			}
		})
	})
}

// choice is a menu button picking one of options into *value.
func choice[T comparable](c *ui.Context, value *T, options []T, label func(T) string) ui.Element {
	return ui.MenuButton(c, label(*value), func(m *ui.Menu) {
		for _, o := range options {
			if m.Item(label(o)).Checked(*value == o).Chosen() {
				*value = o
			}
		}
	})
}

// --- Actions (main thread) ---

// startDownload starts the form's download at the user's request. An update
// message from before belongs to the last download, so it goes; an update
// still running carries on, reporting only what the launch check would.
func (a *App) startDownload() {
	if !a.startJob() {
		return
	}
	if a.ytdlpUpdate.Running {
		a.ytdlpUpdate.Manual, a.ytdlpUpdate.Retry = false, false
	} else {
		a.ytdlpUpdate = ytdlpUpdate{}
	}
}

// startJob starts the form's download, reporting whether it did.
func (a *App) startJob() bool {
	if err := a.runner.Start(a.request(), a.tools); err != nil {
		return false // the form shows why; Download is disabled while it does
	}
	a.job = a.runner.State()
	return true
}

func (a *App) loadCookieFile() {
	path := a.host.ChooseFile("Choose a cookies.txt export")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.cookieNotice = "Couldn't read " + filepath.Base(path) + "."
		a.cookieNoticeFor = a.form.Auth.Cookies
		return
	}
	a.form.Auth.Cookies = string(data)
}

func (a *App) chooseFolder() {
	if dir := a.host.ChooseFolder(a.form.OutputDir); dir != "" {
		a.form.OutputDir = dir
	}
}

func (a *App) locateYtDLP() {
	if path := a.host.ChooseFile("Choose the yt-dlp executable"); path != "" {
		a.settings.YtDLPPath = path
		a.save()
		a.toolsChecked = false
		a.findTools()
	}
}

// persist saves the form's remembered parts when they change.
func (a *App) persist() {
	next := a.settings
	next.Options = a.form.Options
	next.Browser = a.form.Auth.Browser
	next.OutputDir = a.form.OutputDir
	if next != a.settings {
		a.settings = next
		a.save()
	}
}

func (a *App) save() {
	a.store.Save(a.settings) // best effort: losing a preference isn't worth an alert
}

// UpdateYtDLP updates the managed yt-dlp at the user's request: from the
// menu, or after a failed download, which then starts again if a newer
// yt-dlp arrives.
func (a *App) UpdateYtDLP() {
	retry := a.job.Status == job.Failed
	switch {
	case a.ytdlpUpdate.Running:
		// The launch check is already at it: report its outcome as asked for.
		a.ytdlpUpdate.Manual = true
		a.ytdlpUpdate.Retry = a.ytdlpUpdate.Retry || retry
	case !a.toolsChecked:
		// Still looking; the lookup updates a due copy itself.
	case a.tools.YtDLP.Source == tools.Missing:
		a.ytdlpUpdate = ytdlpUpdate{Missing: true}
	case !a.tools.YtDLP.Updatable():
		a.ytdlpUpdate = ytdlpUpdate{Outside: true}
	default:
		a.runUpdate(ytdlpUpdate{Manual: true, Retry: retry})
	}
}

// runUpdate updates the managed yt-dlp on a goroutine; u says who asked.
func (a *App) runUpdate(u ytdlpUpdate) {
	tool := a.tools.YtDLP
	u.Running = true
	a.ytdlpUpdate = u
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
		defer cancel()
		result := a.update(ctx, tool)
		a.host.Update(func() { a.updateFinished(result) })
	}()
}

func (a *App) updateFinished(result ytdlp.UpdateResult) {
	u := a.ytdlpUpdate
	u.Running, u.Done, u.Result = false, true, result
	if result.OK {
		a.settings.LastYtDLPCheck = time.Now()
		a.save()
	}
	if u.Retry && result.Updated && a.job.Status == job.Failed {
		u.Retried = a.startJob()
	}
	a.ytdlpUpdate = u
}

// findTools seeds and looks up tools on a goroutine, then updates yt-dlp if
// a check is due.
func (a *App) findTools() {
	located := a.settings.YtDLPPath
	a.lookups++
	lookup := a.lookups
	go func() {
		a.seed()
		found := a.find(located)
		a.host.Update(func() {
			if lookup != a.lookups {
				return // a later lookup has started; its answer wins
			}
			a.tools = found
			a.toolsChecked = true
			if found.YtDLP.Updatable() && !a.ytdlpUpdate.Running &&
				tools.UpdateDue(a.settings.LastYtDLPCheck, time.Now()) {
				a.runUpdate(ytdlpUpdate{})
			}
		})
	}()
}

// jobChanged is the runner's change callback; it runs on the runner's
// goroutines and copies the new state in on the main thread.
func (a *App) jobChanged() {
	if a.host == nil || !a.jobPending.CompareAndSwap(false, true) {
		return
	}
	a.host.Update(func() {
		a.jobPending.Store(false)
		a.job = a.runner.State()
	})
}

// --- Labels ---

func statusText(s job.State) string {
	switch s.Status {
	case job.Starting:
		return "Starting…"
	case job.Downloading:
		return "Downloading"
	case job.Processing:
		return map[ytdlp.Phase]string{
			ytdlp.Merging:    "Merging video and audio…",
			ytdlp.Extracting: "Extracting audio…",
			ytdlp.Metadata:   "Writing metadata…",
			ytdlp.Subtitles:  "Fetching subtitles…",
		}[s.Phase]
	case job.Cancelling:
		return "Cancelling…"
	case job.Done:
		return "Done"
	case job.Failed:
		return "Failed"
	case job.Cancelled:
		return "Cancelled"
	}
	return "Ready"
}

func heightLabel(h int) string {
	switch h {
	case 0:
		return "Best available"
	case 2160:
		return "2160p (4K)"
	case 1440:
		return "1440p (2K)"
	}
	return fmt.Sprintf("%dp", h)
}

func containerLabel(v request.Container) string {
	if v == request.MKV {
		return "MKV (never re-encodes)"
	}
	return "MP4 (most compatible)"
}

func formatLabel(f request.AudioFormat) string {
	return map[request.AudioFormat]string{
		request.MP3:  "MP3",
		request.M4A:  "M4A (AAC)",
		request.Opus: "Opus",
		request.FLAC: "FLAC (lossless)",
		request.WAV:  "WAV (lossless)",
	}[f]
}

func bitrateLabel(kbps int) string {
	if kbps == request.BestBitrate {
		return "Best available"
	}
	return fmt.Sprintf("%d kbps", kbps)
}

func browserLabel(b request.Browser) string {
	s := string(b)
	return strings.ToUpper(s[:1]) + s[1:]
}

func cookieSummary(text string) string {
	if text == "" {
		return "nothing pasted yet"
	}
	if n := cookies.Count(text); n > 0 {
		return plural(n, "cookie")
	}
	return "no cookies recognised"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// speedText is "3.1MiB/s · ETA 00:21", with whichever parts are known.
func speedText(s job.State) string {
	parts := []string{}
	if s.Speed != "" {
		parts = append(parts, s.Speed)
	}
	if s.ETA != "" {
		parts = append(parts, "ETA "+s.ETA)
	}
	return strings.Join(parts, " · ")
}
