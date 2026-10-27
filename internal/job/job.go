// Package job runs one yt-dlp download at a time and keeps its state.
package job

import (
	"os/exec"
	"sync"

	"github.com/aatng-gh/ytgrab/internal/cookies"
	"github.com/aatng-gh/ytgrab/internal/request"
	"github.com/aatng-gh/ytgrab/internal/tools"
	"github.com/aatng-gh/ytgrab/internal/ytdlp"
)

// Status is where a job is in its life.
type Status string

const (
	Idle        Status = "idle"
	Starting    Status = "starting"
	Downloading Status = "downloading"
	Processing  Status = "processing" // see State.Phase
	Cancelling  Status = "cancelling"
	Done        Status = "done"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
)

// Running reports whether a process is (or is about to be) alive.
func (s Status) Running() bool {
	return s == Starting || s == Downloading || s == Processing || s == Cancelling
}

// MaxLogLines caps State.Log.
const MaxLogLines = 2000

// State is everything the view shows about the current or last job. Runner
// hands out copies, so the view never shares memory with the runner.
type State struct {
	Status Status
	// Phase is set while Status is Processing.
	Phase ytdlp.Phase
	// Progress is 0…1, or -1 when indeterminate.
	Progress   float64
	Speed, ETA string
	Title      string
	// Error is the last error yt-dlp reported, or why the job couldn't start.
	Error string
	// File is the last file yt-dlp wrote, for Reveal in Finder.
	File string
	// Log is the command line and every output line except progress, newest
	// last.
	Log []string
}

// Runner owns the yt-dlp process and the job's state.
type Runner struct {
	// onChange is called after every state change, from any goroutine. The app
	// passes a function that copies State() into the view via Window.Update.
	onChange func()

	mu    sync.Mutex
	state State
	cmd   *exec.Cmd
	jar   *cookies.Jar
}

func NewRunner(onChange func()) *Runner {
	return &Runner{onChange: onChange, state: State{Status: Idle}}
}

// State returns a copy of the current state.
func (r *Runner) State() State {
	panic("TODO")
}

// Start validates req, writes a cookie jar if needed, and launches yt-dlp in
// its own process group. Returns an error only when nothing was started; it
// is also recorded in State.Error.
func (r *Runner) Start(req request.Request, found tools.Found) error {
	panic("TODO")
}

// Cancel asks the process group to stop with SIGTERM, then SIGKILL after
// three seconds.
func (r *Runner) Cancel() {
	panic("TODO")
}

// Close stops any job within about 1.5 s and removes its cookie jar. Called
// when the app quits.
func (r *Runner) Close() {
	panic("TODO")
}
