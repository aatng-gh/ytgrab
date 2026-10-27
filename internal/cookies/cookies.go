// Package cookies handles pasted Netscape cookie jars: counting them, fixing
// their header, and keeping them on disk only for the length of a download.
package cookies

// Count returns the number of cookie lines in text, counting #HttpOnly_ lines
// and ignoring comments and blanks.
func Count(text string) int {
	panic("TODO")
}

// Normalize returns text as yt-dlp accepts it: LF line endings, the Netscape
// header added if missing, and a trailing newline.
func Normalize(text string) string {
	panic("TODO")
}

// Jar is a temporary cookie file: 0600, inside its own 0700 directory whose
// name carries the app's process ID, so SweepStale can tell orphans apart.
type Jar struct {
	Path string
	dir  string
}

// Write stores text in a new Jar. It returns nil when text holds no cookies.
func Write(text string) (*Jar, error) {
	panic("TODO")
}

// Remove overwrites the file and deletes the jar's directory. Safe on nil and
// when called twice.
func (j *Jar) Remove() {
	panic("TODO")
}

// SweepStale removes jars left by app processes that are no longer running,
// after a crash or force quit. Called at launch.
func SweepStale() {
	panic("TODO")
}
