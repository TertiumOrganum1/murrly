#!/usr/bin/env bash
# build-icons.sh — generate murrly.icns from the iconset checked into the repo.
#
# Run via `make icons`. On Linux this is a no-op (iconutil is macOS-only;
# Linux builds don't need the .icns).
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

ICONSET_SRC="$REPO_ROOT/assets/icons/app"
BUILD_DIR="$REPO_ROOT/build"
ICONSET_TMP="$BUILD_DIR/murrly.iconset"
ICNS_OUT="$BUILD_DIR/murrly.icns"

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "build-icons.sh: skipping — iconutil is macOS-only"
    exit 0
fi

if ! command -v iconutil >/dev/null 2>&1; then
    echo "iconutil not found (should ship with macOS Xcode Command Line Tools)" >&2
    exit 1
fi

if [[ ! -d "$ICONSET_SRC" ]] || [[ -z "$(ls -A "$ICONSET_SRC" 2>/dev/null)" ]]; then
    echo "iconset source missing or empty: $ICONSET_SRC" >&2
    exit 1
fi

rm -rf "$ICONSET_TMP"
mkdir -p "$ICONSET_TMP"
cp "$ICONSET_SRC"/*.png "$ICONSET_TMP/"

iconutil -c icns -o "$ICNS_OUT" "$ICONSET_TMP"
echo "Built: $ICNS_OUT"
