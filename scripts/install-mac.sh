#!/usr/bin/env bash
# install-mac.sh — assemble Murrly.app and copy to /Applications.
#
# Idempotent. With --autostart, also registers the .app as a Login Item.
# Requires: bin/murrly already built, build/murrly.icns generated (make icons).
# Ad-hoc codesigns the bundle (free; first launch still triggers Gatekeeper).
set -euo pipefail

APP_NAME="Murrly"
BINARY_NAME="murrly"

AUTOSTART=0
if [[ "${1:-}" == "--autostart" ]]; then
    AUTOSTART=1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

BIN_SRC="$REPO_ROOT/bin/$BINARY_NAME"
WHISPER_BUILD="$REPO_ROOT/third_party/whisper.cpp/build"
BUILD_DIR="$REPO_ROOT/build"
ICNS_SRC="$BUILD_DIR/$BINARY_NAME.icns"
INFO_TMPL="$REPO_ROOT/scripts/templates/Info.plist.tmpl"
VERSION="$(tr -d '[:space:]' < "$REPO_ROOT/VERSION")"

if [[ ! -x "$BIN_SRC" ]]; then
    echo "Binary not found or not executable: $BIN_SRC" >&2
    echo "Run: make build" >&2
    exit 1
fi

if [[ ! -f "$ICNS_SRC" ]]; then
    echo "Icon file not found: $ICNS_SRC" >&2
    echo "Run: make icons" >&2
    exit 1
fi

# Metal shaders are typically embedded in libggml-metal.a
# (GGML_METAL_EMBED_LIBRARY=ON, the default for our build). If a loose
# .metallib exists in the whisper build dir, we ship it inside Resources/
# as a safety net for downstream tools that still expect it.
METALLIB="$(find "$WHISPER_BUILD" -name '*.metallib' 2>/dev/null | head -n1 || true)"

# The multi-inference picker (Ctrl+F11). Optional: if it's missing the app
# still runs and Ctrl+F11 just no-ops. `make build` produces bin/picker.
PICKER_SRC="$REPO_ROOT/bin/picker"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

APP_TMP="$TMP/$APP_NAME.app"
mkdir -p "$APP_TMP/Contents/MacOS" "$APP_TMP/Contents/Resources"

sed "s/@@VERSION@@/$VERSION/g" "$INFO_TMPL" > "$APP_TMP/Contents/Info.plist"
install -m 0755 "$BIN_SRC"  "$APP_TMP/Contents/MacOS/$BINARY_NAME"
if [[ -x "$PICKER_SRC" ]]; then
    install -m 0755 "$PICKER_SRC" "$APP_TMP/Contents/MacOS/murrly-picker"
fi
install -m 0644 "$ICNS_SRC" "$APP_TMP/Contents/Resources/$BINARY_NAME.icns"
if [[ -n "$METALLIB" ]]; then
    install -m 0644 "$METALLIB" "$APP_TMP/Contents/Resources/$(basename "$METALLIB")"
fi

codesign --sign - --force --deep "$APP_TMP" 2>&1 | sed 's/^/codesign: /'

DEST="/Applications/$APP_NAME.app"
if [[ -d "$DEST" ]]; then
    rm -rf "$DEST"
fi
mv "$APP_TMP" "$DEST"
# Clear the EXIT trap: the temp dir is now empty (only contained the .app we moved).
trap - EXIT

echo "Installed: $DEST"

# Drop stale TCC grants. Each rebuild changes the ad-hoc cdhash, so the
# previous Accessibility/Microphone grants no longer apply — but the
# stale entry sits in System Settings looking enabled and confuses the
# user. Resetting forces a clean re-grant against the new cdhash.
tccutil reset Accessibility com.tertiumorganum1.murrly >/dev/null 2>&1 || true
tccutil reset Microphone    com.tertiumorganum1.murrly >/dev/null 2>&1 || true
tccutil reset AppleEvents   com.tertiumorganum1.murrly >/dev/null 2>&1 || true

if [[ "$AUTOSTART" == "1" ]]; then
    osascript -e "tell application \"System Events\" to make login item at end with properties {path:\"$DEST\", hidden:true}" >/dev/null
    echo "Autostart enabled (Login Item added)."
fi

cat <<MSG

Next steps:
  1. Launch from Spotlight (Cmd+Space, type "Murrly") or:
       open -a Murrly
  2. The first launch will:
     - Trigger Gatekeeper (ad-hoc signed). Right-click → Open.
     - Prompt for Microphone access — grant it.
     - Prompt for Accessibility access — grant via System Settings,
       then relaunch the app.
  3. Hold F12 (or fn+F12 on macOS), speak, release.

Config:  ~/Library/Application Support/Murrly/config.toml
Models:  ~/Library/Application Support/Murrly/models/
Log:     ~/Library/Caches/Murrly/murrly.log
MSG
