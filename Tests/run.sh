#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEST_DIR="$(mktemp -d /private/tmp/ytgrab-tests.XXXXXX)"
trap 'rm -rf "$TEST_DIR"' EXIT

if [ -n "${SDKROOT:-}" ]; then
    SDK="$SDKROOT"
else
    SDK=""
    for candidate in MacOSX26.sdk MacOSX26.5.sdk MacOSX15.sdk MacOSX14.sdk; do
        path="/Library/Developer/CommandLineTools/SDKs/$candidate"
        if [ -d "$path" ]; then SDK="$path"; break; fi
    done
    if [ -z "$SDK" ]; then SDK="$(xcrun --show-sdk-path)"; fi
fi

CLANG_MODULE_CACHE_PATH="$TEST_DIR/modules" \
SWIFT_MODULECACHE_PATH="$TEST_DIR/modules" \
swiftc \
    -swift-version 5 \
    -target "$(uname -m)-apple-macos14.0" \
    -sdk "$SDK" \
    -o "$TEST_DIR/LogicTests" \
    "$ROOT/Sources/Models.swift" \
    "$ROOT/Sources/Cookies.swift" \
    "$ROOT/Tests/LogicTests.swift"

"$TEST_DIR/LogicTests"
