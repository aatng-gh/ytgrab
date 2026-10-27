// Command fetch-tools downloads the tools pinned in tools.json (yt-dlp, ffmpeg
// and ffprobe) into resources/darwin-arm64/bin, which MyGo copies to
// Contents/Resources/bin. Each is checked against its SHA-256, and a cached
// copy that doesn't match is downloaded again. MyGo runs it before every build
// (buildCommand in mygo.json).
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// pinsJSON lists the tools, each a GitHub release asset pinned by version and
// SHA-256. Renovate updates it.
//
//go:embed tools.json
var pinsJSON []byte

type pin struct {
	Name    string `json:"name"`
	Repo    string `json:"repo"`
	Version string `json:"version"`
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
}

func (p pin) url() string {
	return "https://github.com/" + p.Repo + "/releases/download/" + p.Version + "/" + p.Asset
}

func loadPins() ([]pin, error) {
	var file struct {
		Tools []pin `json:"tools"`
	}
	err := json.Unmarshal(pinsJSON, &file)
	return file.Tools, err
}

func ytdlpVersion(pins []pin) string {
	for _, p := range pins {
		if p.Name == "yt-dlp" {
			return p.Version
		}
	}
	return ""
}

func main() {
	dir := flag.String("dir", "resources/darwin-arm64/bin", "where to put the tools")
	flag.Parse()
	log.SetFlags(0)

	pins, err := loadPins()
	if err != nil {
		log.Fatalf("tools.json: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for _, p := range pins {
		fetched, err := fetch(ctx, http.DefaultClient, p.url(), filepath.Join(*dir, p.Name), p.SHA256)
		if err != nil {
			log.Fatalf("%s: %v", p.Name, err)
		}
		if fetched {
			log.Printf("Fetched %s %s", p.Name, p.Version)
		}
	}
	// The app compares this with its updatable copy, so an app update can
	// bring a newer yt-dlp than the copy's own updates managed to.
	if err := os.WriteFile(filepath.Join(*dir, "yt-dlp.version"), []byte(ytdlpVersion(pins)+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
}

// fetch makes dest the executable file at url, whose SHA-256 is want,
// reporting whether it had to download it. A download that doesn't match want
// never replaces dest.
func fetch(ctx context.Context, client *http.Client, url, dest, want string) (bool, error) {
	if sha256File(dest) == want {
		return false, nil
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	part, err := os.CreateTemp(dir, "."+filepath.Base(dest)+"-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(part.Name()) // a no-op once renamed

	err = download(ctx, client, url, part)
	err = errors.Join(err, part.Chmod(0o755), part.Close())
	if err != nil {
		return false, err
	}
	if got := sha256File(part.Name()); got != want {
		return false, fmt.Errorf("SHA-256 is %s, want %s", got, want)
	}
	return true, os.Rename(part.Name(), dest)
}

func download(ctx context.Context, client *http.Client, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// sha256File is the hex SHA-256 of the file at path, or "" if it can't be read.
func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
