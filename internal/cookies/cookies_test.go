package cookies

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const cookie = ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tvalue"

func TestCount(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"", 0},
		{cookie, 1},
		{header + "\n" + cookie + "\n\n# comment\n" + cookie, 2},
		{"#HttpOnly_" + cookie, 1},
		{"not a cookie", 0},
		{".youtube.com\tTRUE\t/", 0},
		{strings.ReplaceAll(cookie+"\n"+cookie, "\n", "\r\n"), 2},
	}
	for _, tt := range tests {
		if got := Count(tt.text); got != tt.want {
			t.Errorf("Count(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"adds header", cookie, header + "\n" + cookie + "\n"},
		{"keeps header", header + "\n" + cookie, header + "\n" + cookie + "\n"},
		{"keeps old header", "# HTTP Cookie File\n" + cookie, "# HTTP Cookie File\n" + cookie + "\n"},
		{"HttpOnly line is not a header", "#HttpOnly_" + cookie, header + "\n#HttpOnly_" + cookie + "\n"},
		{"CRLF and padding", "\r\n" + cookie + "\r\n\r\n", header + "\n" + cookie + "\n"},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.want {
			t.Errorf("%s: normalize = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestJar(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	empty, err := Write("# nothing here")
	if err != nil || empty != nil {
		t.Fatalf("Write without cookies = %v, %v; want nil, nil", empty, err)
	}

	jar, err := Write(cookie)
	if err != nil || jar == nil {
		t.Fatalf("Write = %v, %v", jar, err)
	}
	data, err := os.ReadFile(jar.Path)
	if err != nil || string(data) != normalize(cookie) {
		t.Errorf("jar holds %q, %v", data, err)
	}
	assertMode(t, jar.Path, 0o600)
	dir := filepath.Dir(jar.Path)
	assertMode(t, dir, 0o700)
	prefix := "YTGrab-" + strconv.Itoa(os.Getpid()) + "-"
	if !strings.HasPrefix(filepath.Base(dir), prefix) {
		t.Errorf("jar directory %q doesn't start with %q", filepath.Base(dir), prefix)
	}

	jar.Remove()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("jar directory still exists: %v", err)
	}
	jar.Remove()
	(*Jar)(nil).Remove()
}

func TestSweepStale(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	live, err := Write(cookie)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Remove()

	// No process has PID 2^30 on macOS (PIDs top out at 99,999).
	stale := filepath.Join(tmp, "YTGrab-1073741824-x")
	malformed := filepath.Join(tmp, "YTGrab-junk")
	other := filepath.Join(tmp, "Other-1")
	for _, dir := range []string{stale, malformed, other} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cookies.txt"), []byte(cookie), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	SweepStale()

	for dir, want := range map[string]bool{
		filepath.Dir(live.Path): true,
		stale:                   false,
		malformed:               false,
		other:                   true,
	} {
		_, err := os.Stat(dir)
		if exists := err == nil; exists != want {
			t.Errorf("%s exists = %v, want %v", filepath.Base(dir), exists, want)
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode %o, want %o", filepath.Base(path), got, want)
	}
}
