package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

const body = "#!/bin/sh\necho tool\n"

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// server serves body at /tool and counts the requests.
func server(t *testing.T) (*httptest.Server, *atomic.Int32) {
	hits := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/tool" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestFetch(t *testing.T) {
	srv, hits := server(t)
	dir := filepath.Join(t.TempDir(), "bin") // created on demand
	path := filepath.Join(dir, "yt-dlp")

	fetched, err := fetch(context.Background(), srv.Client(), srv.URL+"/tool", path, sum(body))
	if err != nil || !fetched {
		t.Fatalf("fetch = %v, %v", fetched, err)
	}
	if data, _ := os.ReadFile(path); string(data) != body {
		t.Errorf("wrote %q", data)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, %v", info.Mode(), err)
	}

	fetched, err = fetch(context.Background(), srv.Client(), srv.URL+"/tool", path, sum(body))
	if err != nil || fetched || hits.Load() != 1 {
		t.Errorf("a matching copy was fetched again: %v, %v, %d requests", fetched, err, hits.Load())
	}
}

func TestFetchReplacesAStaleCopy(t *testing.T) {
	srv, _ := server(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("older version"), 0o755)

	fetched, err := fetch(context.Background(), srv.Client(), srv.URL+"/tool", filepath.Join(dir, "yt-dlp"), sum(body))
	if err != nil || !fetched {
		t.Fatalf("fetch = %v, %v", fetched, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "yt-dlp")); string(data) != body {
		t.Errorf("kept %q", data)
	}
}

func TestFetchRejects(t *testing.T) {
	srv, _ := server(t)
	for name, tc := range map[string]struct{ url, sha256 string }{
		"wrong checksum": {srv.URL + "/tool", sum("something else")},
		"missing asset":  {srv.URL + "/gone", sum(body)},
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("older version"), 0o755)
		if _, err := fetch(context.Background(), srv.Client(), tc.url, filepath.Join(dir, "yt-dlp"), tc.sha256); err == nil {
			t.Errorf("%s: no error", name)
		}
		if data, _ := os.ReadFile(filepath.Join(dir, "yt-dlp")); string(data) != "older version" {
			t.Errorf("%s: replaced the old copy with %q", name, data)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("%s: left %d files behind", name, len(entries)-1)
		}
	}
}

func TestPins(t *testing.T) {
	pins, err := loadPins()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range pins {
		names[p.Name] = true
		if len(p.SHA256) != 64 || p.Repo == "" || p.Version == "" || p.Asset == "" {
			t.Errorf("incomplete pin %+v", p)
		}
	}
	for _, name := range []string{"yt-dlp", "ffmpeg", "ffprobe"} {
		if !names[name] {
			t.Errorf("no pin for %s", name)
		}
	}
	if v := ytdlpVersion(pins); v == "" {
		t.Error("no yt-dlp version")
	}
}

func TestPinURL(t *testing.T) {
	p := pin{Name: "yt-dlp", Repo: "yt-dlp/yt-dlp", Version: "2026.10.26", Asset: "yt-dlp_macos"}
	if got, want := p.url(), "https://github.com/yt-dlp/yt-dlp/releases/download/2026.10.26/yt-dlp_macos"; got != want {
		t.Errorf("url = %s, want %s", got, want)
	}
}
