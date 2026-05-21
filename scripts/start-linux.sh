#!/usr/bin/env bash
set -euo pipefail

APP_ID="murrly"
BIN="${BIN:-$HOME/.local/bin/$APP_ID}"
LOG_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/murrly"
LOG_FILE="$LOG_DIR/murrly.log"

mkdir -p "$LOG_DIR"

if [[ ! -x "$BIN" ]]; then
	echo "Binary not found or not executable: $BIN" >&2
	echo "Run: make install" >&2
	exit 1
fi

if pgrep -u "$(id -u)" -x "$APP_ID" >/dev/null 2>&1; then
	echo "$APP_ID is already running."
	exit 0
fi

if command -v setsid >/dev/null 2>&1; then
	setsid -f "$BIN" >>"$LOG_FILE" 2>&1
else
	nohup "$BIN" >>"$LOG_FILE" 2>&1 &
	disown "$!" 2>/dev/null || true
fi

echo "Started $APP_ID in background."
echo "Log: $LOG_FILE"
