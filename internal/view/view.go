// Package view is the window: it draws app state with MyGo's ui package and
// turns clicks into calls on the other packages. It holds no download logic.
//
// Threading: View and every action run on the main thread. Work that can be
// slow (finding tools, updating yt-dlp, dialogs aside) runs on a goroutine
// and writes its result back through win.Update.
package view

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/aatng-gh/ytgrab/internal/job"
	"github.com/aatng-gh/ytgrab/internal/request"
	"github.com/aatng-gh/ytgrab/internal/settings"
	"github.com/aatng-gh/ytgrab/internal/tools"
)

// App is the window's state. Fields are only touched on the main thread.
type App struct {
	win    *mygo.Window
	store  *settings.Store
	runner *job.Runner
	dirs   tools.Dirs

	settings settings.Settings
	// form is what the controls edit. Its Options, Auth.Browser and OutputDir
	// are written back to settings when they change.
	form request.Request
	// rangeOn shows the range fields; form.Range is built from them on start.
	rangeOn bool
	rangeStart, rangeEnd string

	// job is the runner's state as of its last change.
	job job.State

	tools        tools.Found
	toolsChecked bool // false until the first lookup finishes
	ytdlpUpdate  YtDLPUpdate

	logOpen bool
	// notice is a one-off message under a section, such as an unreadable
	// cookie file.
	notice string
}

// YtDLPUpdate is the state of the managed yt-dlp's last or current update.
type YtDLPUpdate struct {
	Running bool
	Result  tools.UpdateResult
}

// New loads settings and creates the runner. The window doesn't exist yet;
// see Attach.
func New(store *settings.Store, dirs tools.Dirs) *App {
	a := &App{store: store, dirs: dirs}
	a.runner = job.NewRunner(a.jobChanged)
	// TODO: load settings into a.settings and a.form.
	return a
}

// Attach binds the app to its window and starts the background lookup: seed
// the managed yt-dlp, find tools, then update yt-dlp if due.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	// TODO
}

// Close stops any job and removes its cookie jar. Called when the app quits.
func (a *App) Close() {
	a.runner.Close()
}

// View builds the window: banner, the four sections, footer, log drawer.
func (a *App) View(c *ui.Context) {
	// TODO: layout. Sections are disabled while a.job.Status.Running().
	a.missingToolsBanner(c)
	a.sourceSection(c)
	a.formatSection(c)
	a.signInSection(c)
	a.destinationSection(c)
	a.footer(c)
	a.logDrawer(c)
}

// --- Sections ---

// missingToolsBanner shows when the lookup found no yt-dlp or no ffmpeg,
// with Locate… for yt-dlp.
func (a *App) missingToolsBanner(c *ui.Context) {}

// sourceSection: URL with Paste, playlist toggle, range toggle and fields.
func (a *App) sourceSection(c *ui.Context) {}

// formatSection: Video/Audio, then resolution and container, or format and
// bitrate (disabled for lossless); metadata and subtitle toggles.
func (a *App) formatSection(c *ui.Context) {}

// signInSection: None / Paste cookies / From browser, and their fields.
func (a *App) signInSection(c *ui.Context) {}

// destinationSection: the output folder with Choose….
func (a *App) destinationSection(c *ui.Context) {}

// footer: status, progress, speed and ETA, error detail or yt-dlp update
// message; Log, Reveal in Finder, Update yt-dlp (after a failure, when the
// copy is updatable), and Download or Cancel.
func (a *App) footer(c *ui.Context) {}

// logDrawer: the log, newest at the bottom, with Copy.
func (a *App) logDrawer(c *ui.Context) {}

// --- Actions (main thread) ---

func (a *App) startDownload()  {}
func (a *App) cancelDownload() {}
func (a *App) pasteURL()       {}
func (a *App) pasteCookies()   {}
func (a *App) loadCookieFile() {}
func (a *App) chooseFolder()   {}
func (a *App) locateYtDLP()    {}
func (a *App) revealFile()     {}
func (a *App) copyLog()        {}

// saveSettings writes the remembered parts of the form to the store.
func (a *App) saveSettings() {}

// UpdateYtDLP updates the managed yt-dlp now. Also used by the menu.
func (a *App) UpdateYtDLP() {}

// findTools seeds and looks up tools on a goroutine, then updates a.tools.
func (a *App) findTools() {}

// jobChanged is the runner's change callback; it runs on the runner's
// goroutines and copies the new state in on the main thread.
func (a *App) jobChanged() {
	if a.win == nil {
		return
	}
	a.win.Update(func() { a.job = a.runner.State() })
}
