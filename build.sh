#!/bin/bash
# Builds YTGrab.app. Needs only the Command Line Tools (no Xcode, no SwiftPM).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
APP="$ROOT/build/YTGrab.app"
STAGED_APP="$ROOT/build/.YTGrab.app.staging"
DEPLOY_TARGET="14.0"
ARCH="$(uname -m)"
SWIFTC="swiftc"
MODULE_CACHE="${TMPDIR:-/private/tmp}/YTGrab-module-cache"
export CLANG_MODULE_CACHE_PATH="$MODULE_CACHE"
export SWIFT_MODULECACHE_PATH="$MODULE_CACHE"

# Ask xcrun for the macOS SDK paired with the selected developer toolchain.
# The explicit SDK name matters on machines that have both Xcode and standalone
# Command Line Tools: an unqualified lookup can return a CLT SDK that is newer
# than, and incompatible with, Xcode's Swift compiler.
pick_sdk() {
    if [ -n "${SDKROOT:-}" ]; then echo "$SDKROOT"; return; fi
    xcrun --sdk macosx --show-sdk-path
}
SDK="$(pick_sdk)"

# Standalone yt-dlp/ffmpeg/ffprobe (arm64), embedded so the built .app runs on
# another Apple Silicon Mac with nothing pre-installed. Cached under
# Resources/vendor/ after the first fetch — delete that folder to re-pull.
VENDOR="$ROOT/Resources/vendor"

# renovate: datasource=github-release-attachments depName=yt-dlp packageName=yt-dlp/yt-dlp versioning=loose
YTDLP_VERSION="2026.08.19"
YTDLP_SHA256="0f192b7ec147ab6288885d6351d9ab67367640029b4377576ef46dd79cf7b202"

# renovate: datasource=github-release-attachments depName=ffmpeg packageName=eugeneware/ffmpeg-static versioning=loose
FFMPEG_VERSION="b6.1.1"
FFMPEG_SHA256="a90e3db6a3fd35f6074b013f948b1aa45b31c6375489d39e572bea3f18336584"

# renovate: datasource=github-release-attachments depName=ffprobe packageName=eugeneware/ffmpeg-static versioning=loose
FFPROBE_VERSION="b6.1.1"
FFPROBE_SHA256="bb2db6f5d8cef919da12fbf592119a987202a8c060a886f3cab091f9cab90b64"

YTDLP_URL="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_macos"
FFMPEG_URL="https://github.com/eugeneware/ffmpeg-static/releases/download/${FFMPEG_VERSION}/ffmpeg-darwin-arm64"
FFPROBE_URL="https://github.com/eugeneware/ffmpeg-static/releases/download/${FFPROBE_VERSION}/ffprobe-darwin-arm64"

verify_sha256() {
    local file="$1" expected="$2"
    printf '%s  %s\n' "$expected" "$file" | shasum -a 256 -c - >/dev/null
}

fetch_vendor_tools() {
    mkdir -p "$VENDOR"
    local name url expected dest download
    for spec in \
        "yt-dlp|$YTDLP_URL|$YTDLP_SHA256" \
        "ffmpeg|$FFMPEG_URL|$FFMPEG_SHA256" \
        "ffprobe|$FFPROBE_URL|$FFPROBE_SHA256"; do
        IFS='|' read -r name url expected <<< "$spec"
        dest="$VENDOR/$name"
        if [ -x "$dest" ]; then
            if ! verify_sha256 "$dest" "$expected"; then
                echo "  $name failed its SHA-256 check; remove it and rebuild." >&2
                return 1
            fi
            continue
        fi
        echo "  Fetching ${name}…"
        download="$VENDOR/.$name.download"
        curl --fail --show-error --location --output "$download" "$url"
        if ! verify_sha256 "$download" "$expected"; then
            rm -f "$download"
            echo "  $name download failed its SHA-256 check." >&2
            return 1
        fi
        chmod +x "$download"
        mv "$download" "$dest"
    done
}

rm -rf "$STAGED_APP"
cleanup_staging() {
    local status=$?
    rm -rf "$STAGED_APP"
    exit "$status"
}
trap cleanup_staging EXIT
mkdir -p "$STAGED_APP/Contents/MacOS" "$STAGED_APP/Contents/Resources"
cp "$ROOT/Resources/Info.plist" "$STAGED_APP/Contents/Info.plist"

echo "Embedding standalone yt-dlp/ffmpeg/ffprobe…"
fetch_vendor_tools
mkdir -p "$STAGED_APP/Contents/Resources/bin"
cp "$VENDOR/yt-dlp" "$VENDOR/ffmpeg" "$VENDOR/ffprobe" "$STAGED_APP/Contents/Resources/bin/"
for name in yt-dlp ffmpeg ffprobe; do
    codesign --force --sign - --identifier "local.ytgrab.vendor.$name" \
        "$STAGED_APP/Contents/Resources/bin/$name"
done

echo "Drawing icon…"
swift "$ROOT/Resources/make-icon.swift" \
    "$STAGED_APP/Contents/Resources/AppIcon.icns" \
    "$ROOT/Resources/AppIcon.png" \
    || echo "  (skipped — app will use the generic icon)"

echo "Compiling for $ARCH (macOS $DEPLOY_TARGET+)…"
"$SWIFTC" \
    -O \
    -swift-version 5 \
    -parse-as-library \
    -target "${ARCH}-apple-macos${DEPLOY_TARGET}" \
    -sdk "$SDK" \
    -framework SwiftUI -framework AppKit \
    -o "$STAGED_APP/Contents/MacOS/YTGrab" \
    "$ROOT/Sources"/*.swift

echo "Signing (ad-hoc)…"
codesign --force --sign - "$STAGED_APP"

# Replace the previous app only after every build step has succeeded.
rm -rf "$APP"
mv "$STAGED_APP" "$APP"

echo
echo "Built: $APP"
echo "Run it with:  open \"$APP\""
