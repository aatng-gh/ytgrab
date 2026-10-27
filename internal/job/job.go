// Package job runs one yt-dlp download at a time and keeps its state.
package job

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

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

// maxLogLines caps State.Log.
const maxLogLines = 2000

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
	// Error is the last error yt-dlp reported, or why the job failed.
	Error string
	// File is the last file yt-dlp wrote, for Reveal in Finder.
	File string
	// Log is the command line and every output line except progress, newest
	// last, ending with the outcome.
	Log []string
}

// Runner owns the yt-dlp process and the job's state.
type Runner struct {
	// onChange is called after every state change, from any goroutine. The app
	// passes a function that copies State() into the view via Window.Update.
	onChange func()
	// killAfter is how long Cancel waits after SIGTERM before SIGKILL.
	killAfter time.Duration

	mu        sync.Mutex
	state     State
	proc      *proc // the running yt-dlp, nil between jobs
	jar       *cookies.Jar
	cancelled bool
}

// proc is one yt-dlp process and what winds it up.
type proc struct {
	cmd *exec.Cmd
	// out and errs are the read ends of its stdout and stderr.
	out, errs *os.File
	// reaped is set, under Runner.mu, the moment Wait returns: from then on
	// the process-group ID is free for the system to reuse, so it must not
	// be signalled.
	reaped bool
	// exited is closed once the job is fully wound up.
	exited chan struct{}
}

func NewRunner(onChange func()) *Runner {
	return &Runner{onChange: onChange, killAfter: 3 * time.Second, state: State{Status: Idle}}
}

// State returns a copy of the current state.
func (r *Runner) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.Log = append([]string(nil), s.Log...)
	return s
}

// update changes the state under the lock, then reports the change.
func (r *Runner) update(fn func(s *State)) {
	r.mu.Lock()
	fn(&r.state)
	r.mu.Unlock()
	r.onChange()
}

func (s *State) log(line string) {
	s.Log = append(s.Log, line)
	if over := len(s.Log) - maxLogLines; over > 0 {
		s.Log = append(s.Log[:0], s.Log[over:]...)
	}
}

// Start validates req, writes a cookie jar if needed, and launches yt-dlp in
// its own process group. A request that can't run returns an error and
// leaves the state alone; a failure to launch is recorded as Failed.
func (r *Runner) Start(req request.Request, found tools.Found) error {
	if problems := req.Validate(); len(problems) > 0 {
		return errors.New(problems[0].Message)
	}
	if !found.Ready() {
		return errors.New("yt-dlp or ffmpeg is missing")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status.Running() {
		return errors.New("a download is already running")
	}
	r.state = State{Status: Starting, Progress: -1}
	r.cancelled = false

	fail := func(msg string) error {
		r.jar.Remove()
		r.jar = nil
		r.state.Status, r.state.Error, r.state.Progress = Failed, msg, 0
		r.state.log("— " + msg + " —")
		go r.onChange()
		return nil
	}

	inv := ytdlp.Invocation{FFmpegDir: filepath.Dir(found.FFmpeg.Path)}
	if req.Auth.Mode == request.PastedCookies {
		jar, err := cookies.Write(req.Auth.Cookies)
		if err != nil {
			return fail("Couldn't store the cookies: " + err.Error())
		}
		r.jar = jar
		inv.CookieJar = jar.Path
	}
	args := ytdlp.Args(req, inv)
	r.state.log("$ " + ytdlp.CommandLine(found.YtDLP.Path, args))

	cmd := exec.Command(found.YtDLP.Path, args...)
	// Not the output folder: it may not exist yet, and -P creates it.
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "PATH="+tools.SearchPath(inv.FFmpegDir))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// os.Pipe rather than StdoutPipe: ffmpeg, a grandchild, inherits the write
	// ends and may hold them open after yt-dlp exits, so EOF can't be waited
	// for. Closing our read ends after exit is what ends the readers.
	outR, outW, err := os.Pipe()
	if err != nil {
		return fail("Couldn't start yt-dlp: " + err.Error())
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return fail("Couldn't start yt-dlp: " + err.Error())
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	err = cmd.Start()
	outW.Close()
	errW.Close()
	if err != nil {
		outR.Close()
		errR.Close()
		return fail("Couldn't start yt-dlp: " + err.Error())
	}

	r.proc = &proc{cmd: cmd, out: outR, errs: errR, exited: make(chan struct{})}
	go r.onChange()
	go r.wait(r.proc)
	return nil
}

func (r *Runner) read(f *os.File, done *sync.WaitGroup) {
	defer done.Done()
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadString('\n')
		if line = strings.TrimSpace(line); line != "" {
			r.handle(line)
		}
		if err != nil {
			return
		}
	}
}

func (r *Runner) handle(line string) {
	parsed := ytdlp.ParseLine(line)
	r.update(func(s *State) {
		if p := parsed.Progress; p != nil {
			if s.Status == Starting || s.Status == Downloading || s.Status == Processing {
				s.Status, s.Phase = Downloading, ""
				s.Progress = -1
				if p.Percent >= 0 {
					s.Progress = min(max(p.Percent/100, 0), 1)
				}
				s.Speed, s.ETA = p.Speed, p.ETA
				if p.Title != "" {
					s.Title = p.Title
				}
			}
			return
		}
		s.log(line)
		if parsed.File != "" {
			s.File = parsed.File
		}
		if parsed.Error != "" {
			s.Error = parsed.Error
		}
		if parsed.Phase != "" && s.Status != Cancelling && s.Status.Running() {
			s.Status, s.Phase = Processing, parsed.Phase
			s.Progress, s.Speed, s.ETA = -1, "", ""
		}
	})
}

// wait reads p's output until it exits, then records the outcome.
func (r *Runner) wait(p *proc) {
	var readers sync.WaitGroup
	readers.Add(2)
	go r.read(p.out, &readers)
	go r.read(p.errs, &readers)

	err := p.cmd.Wait()
	r.mu.Lock()
	p.reaped = true
	r.mu.Unlock()

	drained := make(chan struct{})
	go func() { readers.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(200 * time.Millisecond):
		// Still reading. If a grandchild, such as an ffmpeg yt-dlp didn't stop,
		// holds the pipes, the group is alive, so its ID is still ours: stop
		// it, and EOF follows. Otherwise the readers are only behind.
		if syscall.Kill(-p.cmd.Process.Pid, 0) == nil {
			syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-drained:
		case <-time.After(5 * time.Second): // a writer outside the group
		}
	}
	p.out.Close()
	p.errs.Close()
	<-drained

	// The jar goes before the outcome is published, so nothing that sees the
	// job finished can find it still on disk.
	r.mu.Lock()
	jar := r.jar
	r.jar = nil
	r.mu.Unlock()
	jar.Remove()

	r.mu.Lock()
	r.proc = nil
	s := &r.state
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		s.Status, s.Progress = Done, 1
	case r.cancelled:
		s.Status = Cancelled
	case errors.As(err, &exitErr) && !exitErr.Exited():
		// A crash, or something else killing it: not the user's cancel.
		s.Status = Failed
		sig := exitErr.Sys().(syscall.WaitStatus).Signal()
		s.Error = fmt.Sprintf("yt-dlp stopped unexpectedly (%v).", sig)
	default:
		s.Status = Failed
		if s.Error == "" {
			code := -1
			if exitErr != nil {
				code = exitErr.ExitCode()
			}
			s.Error = fmt.Sprintf("yt-dlp exited with code %d.", code)
		}
	}
	if s.Status != Done {
		s.Progress = 0
	}
	s.Phase, s.Speed, s.ETA = "", "", ""
	outcome := map[Status]string{Done: "Done", Cancelled: "Cancelled", Failed: s.Error}[s.Status]
	s.log("— " + outcome + " —")
	r.mu.Unlock()

	close(p.exited)
	r.onChange()
}

// Cancel asks the process group to stop with SIGTERM, then SIGKILL after
// killAfter.
func (r *Runner) Cancel() {
	r.mu.Lock()
	p := r.proc
	if p == nil || p.reaped || r.cancelled {
		// Nothing to stop: a reaped yt-dlp has already decided the outcome.
		r.mu.Unlock()
		return
	}
	r.cancelled = true
	r.state.Status = Cancelling
	r.state.Speed, r.state.ETA = "", ""
	r.mu.Unlock()
	r.onChange()

	r.signal(p, syscall.SIGTERM)
	go func() {
		select {
		case <-p.exited:
		case <-time.After(r.killAfter):
			r.signal(p, syscall.SIGKILL)
		}
	}()
}

// signal sends sig to p's process group, unless yt-dlp has been reaped and
// the group's ID may already belong to someone else.
func (r *Runner) signal(p *proc, sig syscall.Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !p.reaped {
		syscall.Kill(-p.cmd.Process.Pid, sig)
	}
}

// Close stops any job within about 1.5 s and removes its cookie jar. Called
// when the app quits.
func (r *Runner) Close() {
	r.mu.Lock()
	p := r.proc
	r.mu.Unlock()
	if p == nil {
		return
	}
	r.Cancel()
	select {
	case <-p.exited:
	case <-time.After(1500 * time.Millisecond):
		r.signal(p, syscall.SIGKILL)
		<-p.exited
	}
}
