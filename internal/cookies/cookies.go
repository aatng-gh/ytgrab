// Package cookies handles pasted Netscape cookie jars: counting them, fixing
// their header, and keeping them on disk only for the length of a download.
//
// On APFS, overwriting a file before deleting it doesn't scrub the old blocks
// (the filesystem is copy-on-write). The real protection is the private
// directory and prompt deletion; the overwrite only helps on other volumes.
package cookies

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// header is the first line yt-dlp requires of a cookie jar; headers are all
// the first lines it accepts.
const header = "# Netscape HTTP Cookie File"

var headers = []string{header, "# HTTP Cookie File"}

// httpOnlyPrefix marks HttpOnly cookies in Chromium exports. yt-dlp strips it,
// so these lines are cookies (for YouTube, usually the session ones), not
// comments.
const httpOnlyPrefix = "#HttpOnly_"

// dirPrefix starts the name of every jar directory; the app's PID follows.
const dirPrefix = "YTGrab-"

// Count returns the number of cookie lines in text, counting #HttpOnly_ lines
// and ignoring comments and blanks.
func Count(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, httpOnlyPrefix)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// domain, flag, path, secure, expiry, name[, value]
		if len(strings.Split(line, "\t")) >= 6 {
			n++
		}
	}
	return n
}

// normalize returns text as yt-dlp accepts it: LF line endings, the Netscape
// header added if missing, and a trailing newline.
func normalize(text string) string {
	body := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	first, _, _ := strings.Cut(body, "\n")
	hasHeader := false
	for _, h := range headers {
		if strings.HasPrefix(first, h) {
			hasHeader = true
		}
	}
	if !hasHeader {
		body = header + "\n" + body
	}
	return body + "\n"
}

// Jar is a temporary cookie file: 0600, inside its own 0700 directory whose
// name carries the app's process ID, so SweepStale can tell orphans apart.
type Jar struct {
	Path string
	dir  string
}

// Write stores text in a new Jar. It returns nil when text holds no cookies.
func Write(text string) (*Jar, error) {
	if Count(text) == 0 {
		return nil, nil
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	dir := filepath.Join(os.TempDir(), dirPrefix+strconv.Itoa(os.Getpid())+"-"+hex.EncodeToString(suffix))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	jar := &Jar{Path: filepath.Join(dir, "cookies.txt"), dir: dir}
	// Created 0600 up front so the contents are never readable by others.
	f, err := os.OpenFile(jar.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = f.WriteString(normalize(text))
		err = errors.Join(err, f.Close())
	}
	if err != nil {
		jar.Remove()
		return nil, err
	}
	return jar, nil
}

// Remove overwrites the file and deletes the jar's directory. Safe on nil and
// when called twice.
func (j *Jar) Remove() {
	if j == nil {
		return
	}
	shred(j.Path)
	os.RemoveAll(j.dir)
}

func shred(path string) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		f.Write(make([]byte, info.Size()))
		f.Sync()
	}
}

// SweepStale removes jars left by app processes that are no longer running,
// after a crash or force quit. Called at launch.
func SweepStale() {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), dirPrefix)
		if !ok || !e.IsDir() {
			continue
		}
		pidText, _, _ := strings.Cut(name, "-")
		if pid, err := strconv.Atoi(pidText); err == nil && running(pid) {
			continue
		}
		dir := filepath.Join(tmp, e.Name())
		shred(filepath.Join(dir, "cookies.txt"))
		os.RemoveAll(dir)
	}
}

func running(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
