#!/usr/bin/env bash
set -euo pipefail

APP_ID="murrly"
APP_NAME="Murrly"
AUTOSTART="${AUTOSTART:-0}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

BIN_SRC="${BIN_SRC:-$REPO_ROOT/bin/murrly}"
PICKER_SRC="${PICKER_SRC:-$REPO_ROOT/bin/picker}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
BIN_DEST="$BIN_DIR/$APP_ID"
PICKER_DEST="$BIN_DIR/$APP_ID-picker"
INSTALL_DATA_DIR="${INSTALL_DATA_DIR:-$HOME/.local/share/murrly}"

APP_DIR="$HOME/.local/share/applications"
ICON_BASE="$HOME/.local/share/icons/hicolor"
AUTOSTART_DIR="$HOME/.config/autostart"
LEGACY_SERVICE="$HOME/.config/systemd/user/murrly.service"

if [[ ! -x "$BIN_SRC" ]]; then
	echo "Binary not found or not executable: $BIN_SRC" >&2
	echo "Run: make build" >&2
	exit 1
fi

write_desktop_file() {
	local path="$1"
	local autostart="$2"

	cat >"$path" <<EOF
[Desktop Entry]
Type=Application
Name=$APP_NAME
Comment=Murrly — local push-to-talk speech-to-text
Exec=$BIN_DEST
Icon=$APP_ID
Terminal=false
Categories=Other;
StartupNotify=false
EOF

	if [[ "$autostart" == "1" ]]; then
		cat >>"$path" <<EOF
X-GNOME-Autostart-enabled=true
EOF
	fi
}

remove_legacy_service() {
	if [[ ! -e "$LEGACY_SERVICE" ]]; then
		return
	fi

	if command -v systemctl >/dev/null 2>&1; then
		systemctl --user disable --now murrly.service >/dev/null 2>&1 || true
	fi
	rm -f "$LEGACY_SERVICE"
	if command -v systemctl >/dev/null 2>&1; then
		systemctl --user daemon-reload >/dev/null 2>&1 || true
	fi
	echo "Removed legacy user service: $LEGACY_SERVICE"
}

remove_legacy_service

mkdir -p "$BIN_DIR" "$APP_DIR" "$INSTALL_DATA_DIR/models"
install -m 0755 "$BIN_SRC" "$BIN_DEST"

# The multi-inference variant picker, spawned by murrly via Ctrl+F11.
# Optional: a single-model (count=1) install has no picker to run, but we
# ship it whenever it was built so switching multi_inference_count later
# Just Works without a reinstall.
if [[ -x "$PICKER_SRC" ]]; then
	install -m 0755 "$PICKER_SRC" "$PICKER_DEST"
fi

# Install the Linux-specific colored cat-head app icon at every size we
# ship, so the DE can pick the correct resolution for the launcher, app
# switcher, and any other surface. The full-body British Shorthair in
# assets/icons/app/ is reserved for the macOS .app bundle and .icns.
# Tray icons live alongside the binary (embedded in
# cmd/murrly/assets/tray/*.png).
for size in 22 32 64 128 256; do
	src="$REPO_ROOT/assets/linux/cat_head/cat_head_${size}x${size}.png"
	[[ -f "$src" ]] || continue
	size_dir="$ICON_BASE/${size}x${size}/apps"
	mkdir -p "$size_dir"
	install -m 0644 "$src" "$size_dir/$APP_ID.png"
done

if compgen -G "$REPO_ROOT/models/*.bin" >/dev/null; then
	install -m 0644 "$REPO_ROOT"/models/*.bin "$INSTALL_DATA_DIR/models/"
fi

write_desktop_file "$APP_DIR/$APP_ID.desktop" 0

if [[ "$AUTOSTART" == "1" ]]; then
	mkdir -p "$AUTOSTART_DIR"
	write_desktop_file "$AUTOSTART_DIR/$APP_ID.desktop" 1
	echo "Autostart enabled: $AUTOSTART_DIR/$APP_ID.desktop"
else
	echo "Autostart not enabled. Run 'make autostart' to enable it."
fi

if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi

# gtk-update-icon-cache needs an index.theme. The user-local hicolor dir
# is often empty before any app has installed there, so seed it from the
# system theme (54KB, just the size/context metadata — no icons).
SYS_INDEX="/usr/share/icons/hicolor/index.theme"
USER_INDEX="$ICON_BASE/index.theme"
if [[ ! -f "$USER_INDEX" && -f "$SYS_INDEX" ]]; then
	install -m 0644 "$SYS_INDEX" "$USER_INDEX"
fi

if command -v gtk-update-icon-cache >/dev/null 2>&1; then
	gtk-update-icon-cache --force "$ICON_BASE" >/dev/null 2>&1 || true
fi

cat <<EOF
Installed:
  Binary:  $BIN_DEST
  Launcher: $APP_DIR/$APP_ID.desktop
  Model dir: $INSTALL_DATA_DIR/models

Start it from the application menu or run:
  $BIN_DEST
EOF
