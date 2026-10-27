#!/bin/bash
# Downloads the yt-dlp, ffmpeg and ffprobe that ship inside the app into
# resources/darwin-arm64/bin, which MyGo copies to Contents/Resources/bin.
# Each is pinned and checked against its SHA-256; a cached copy that doesn't
# match (after a version bump) is downloaded again. Renovate updates the pins.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/resources/darwin-arm64/bin"

# renovate: datasource=github-release-attachments depName=yt-dlp packageName=yt-dlp/yt-dlp versioning=loose
YTDLP_VERSION="2026.10.26"
YTDLP_SHA256="0f922c15ccc0b83eefa1c8bc9104e434318c4bf16e408e1ee93b7745a6efe0aa"

# renovate: datasource=github-release-attachments depName=ffmpeg packageName=eugeneware/ffmpeg-static versioning=loose
FFMPEG_VERSION="b6.1.1"
FFMPEG_SHA256="a90e3db6a3fd35f6074b013f948b1aa45b31c6375489d39e572bea3f18336584"

# renovate: datasource=github-release-attachments depName=ffprobe packageName=eugeneware/ffmpeg-static versioning=loose
FFPROBE_VERSION="b6.1.1"
FFPROBE_SHA256="bb2db6f5d8cef919da12fbf592119a987202a8c060a886f3cab091f9cab90b64"

matches() {
    [ -f "$1" ] && printf '%s  %s\n' "$2" "$1" | shasum -a 256 -c - >/dev/null 2>&1
}

fetch() {
    local name="$1" url="$2" sha="$3"
    local dest="$BIN/$name" part="$BIN/.$name.part"
    if matches "$dest" "$sha"; then
        return
    fi
    echo "Fetching ${name}…"
    curl --fail --show-error --silent --location --output "$part" "$url"
    if ! matches "$part" "$sha"; then
        rm -f "$part"
        echo "$name failed its SHA-256 check." >&2
        exit 1
    fi
    chmod 755 "$part"
    mv "$part" "$dest"
}

mkdir -p "$BIN"
fetch yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/$YTDLP_VERSION/yt-dlp_macos" "$YTDLP_SHA256"
# The app compares this with its updatable copy, so an app update can bring a
# newer yt-dlp than the copy's own updates managed to.
echo "$YTDLP_VERSION" > "$BIN/yt-dlp.version"
fetch ffmpeg "https://github.com/eugeneware/ffmpeg-static/releases/download/$FFMPEG_VERSION/ffmpeg-darwin-arm64" "$FFMPEG_SHA256"
fetch ffprobe "https://github.com/eugeneware/ffmpeg-static/releases/download/$FFPROBE_VERSION/ffprobe-darwin-arm64" "$FFPROBE_SHA256"
