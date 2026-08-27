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

# SwiftUI's newest SDK declares @State and friends as macros, and the macro
# plugin (libSwiftUIMacros.dylib) only ships with Xcode — not with the Command
# Line Tools. So prefer an SDK that still declares them as property wrappers
# unless that plugin is actually present.
pick_sdk() {
    if [ -n "${SDKROOT:-}" ]; then echo "$SDKROOT"; return; fi
    local toolchain_plugins
    toolchain_plugins="$(dirname "$(xcrun --find swiftc)")/../lib/swift/host/plugins"
    if [ -f "$toolchain_plugins/libSwiftUIMacros.dylib" ]; then
        xcrun --show-sdk-path; return
    fi
    for candidate in MacOSX26.5.sdk MacOSX26.sdk MacOSX15.sdk MacOSX14.sdk; do
        local path="/Library/Developer/CommandLineTools/SDKs/$candidate"
        [ -d "$path" ] && { echo "$path"; return; }
    done
    xcrun --show-sdk-path
}
SDK="$(pick_sdk)"

# Standalone yt-dlp/ffmpeg/ffprobe (arm64), embedded so the built .app runs on
# another Apple Silicon Mac with nothing pre-installed. Cached under
# Resources/vendor/ after the first fetch — delete that folder to re-pull.
VENDOR="$ROOT/Resources/vendor"
YTDLP_URL="https://github.com/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp_macos"
FFMPEG_URL="https://github.com/eugeneware/ffmpeg-static/releases/download/b6.1.1/ffmpeg-darwin-arm64"
FFPROBE_URL="https://github.com/eugeneware/ffmpeg-static/releases/download/b6.1.1/ffprobe-darwin-arm64"
YTDLP_SHA256="0f192b7ec147ab6288885d6351d9ab67367640029b4377576ef46dd79cf7b202"
FFMPEG_SHA256="5d04dd8860929813e49c0e4411e153be69ca51e1fc0b5cb9b464b8e6f3be1ff6"
FFPROBE_SHA256="ccaf8e18367e39c43ba0371976d26155fed602a1ce71aee929b8dff526dc2d28"

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
        echo "  Fetching $name…"
        download="$VENDOR/.$name.download"
        curl --fail --show-error --location --output "$download" "$url"
        chmod +x "$download"
        codesign --force --sign - --identifier "local.ytgrab.vendor.$name" "$download"
        if ! verify_sha256 "$download" "$expected"; then
            rm -f "$download"
            echo "  $name download failed its SHA-256 check." >&2
            return 1
        fi
        mv "$download" "$dest"
    done
}

rm -rf "$STAGED_APP"
trap 'rm -rf "$STAGED_APP"' EXIT
mkdir -p "$STAGED_APP/Contents/MacOS" "$STAGED_APP/Contents/Resources"
cp "$ROOT/Resources/Info.plist" "$STAGED_APP/Contents/Info.plist"

echo "Embedding standalone yt-dlp/ffmpeg/ffprobe…"
fetch_vendor_tools
mkdir -p "$STAGED_APP/Contents/Resources/bin"
cp "$VENDOR/yt-dlp" "$VENDOR/ffmpeg" "$VENDOR/ffprobe" "$STAGED_APP/Contents/Resources/bin/"

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
