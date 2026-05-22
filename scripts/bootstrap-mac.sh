#!/usr/bin/env bash
# bootstrap-mac.sh — install deps and assemble a working Murrly.app on macOS.
# Apple Silicon target. Intel may work but is not verified.
#
# Env knobs:
#   MODEL=large-v3     # whisper model to download
#   AUTOSTART=0|1      # register Login Item after install
#   INSTALL_APP=1      # 0 to skip the .app install (build only)
set -euo pipefail

MODEL="${MODEL:-large-v3}"
INSTALL_APP="${INSTALL_APP:-1}"
AUTOSTART="${AUTOSTART:-0}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "bootstrap-mac.sh: macOS only. For Linux, use scripts/bootstrap-ubuntu.sh." >&2
    exit 1
fi

if ! command -v brew >/dev/null 2>&1; then
    cat >&2 <<'MSG'
Homebrew is required to install build dependencies.
Install from https://brew.sh, then re-run this script.
MSG
    exit 1
fi

echo "==> Installing build dependencies via Homebrew..."
brew install cmake portaudio go librsvg

echo "==> Building whisper.cpp with Metal acceleration..."
make whisper

echo "==> Downloading model: $MODEL"
make model MODEL="$MODEL"

DATA_DIR="$HOME/Library/Application Support/Murrly/models"
mkdir -p "$DATA_DIR"
install -m 0644 "models/ggml-$MODEL.bin" "$DATA_DIR/"
echo "Model staged: $DATA_DIR/ggml-$MODEL.bin"

echo "==> Building murrly binary..."
make build

echo "==> Generating icons..."
make icons

if [[ "$INSTALL_APP" == "1" ]]; then
    echo "==> Assembling and installing Murrly.app..."
    if [[ "$AUTOSTART" == "1" ]]; then
        scripts/install-mac.sh --autostart
    else
        scripts/install-mac.sh
    fi
fi

cat <<MSG

Done.

App:    /Applications/Murrly.app
Binary: $REPO_ROOT/bin/murrly
Model:  $DATA_DIR/ggml-$MODEL.bin

To start:
  open -a Murrly

First-run hints:
  - Gatekeeper bypass: right-click Murrly.app → Open the first time.
  - Grant Microphone permission when macOS asks.
  - Grant Accessibility via System Settings → Privacy & Security → Accessibility,
    then relaunch the app.
MSG
