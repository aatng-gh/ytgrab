#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEST_DIR="$(mktemp -d /private/tmp/ytgrab-tests.XXXXXX)"
trap 'rm -rf "$TEST_DIR"' EXIT

if [ -n "${SDKROOT:-}" ]; then
    SDK="$SDKROOT"
else
    SDK="$(xcrun --sdk macosx --show-sdk-path)"
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
    "$ROOT/Sources/ToolLocator.swift" \
    "$ROOT/Sources/YtdlpUpdater.swift" \
    "$ROOT/Tests/LogicTests.swift"

"$TEST_DIR/LogicTests"
