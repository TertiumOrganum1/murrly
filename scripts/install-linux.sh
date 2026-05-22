#!/usr/bin/env bash
set -euo pipefail

APP_ID="murrly"
APP_NAME="Murrly"
AUTOSTART="${AUTOSTART:-0}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

BIN_SRC="${BIN_SRC:-$REPO_ROOT/bin/murrly}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
BIN_DEST="$BIN_DIR/$APP_ID"
INSTALL_DATA_DIR="${INSTALL_DATA_DIR:-$HOME/.local/share/murrly}"

APP_DIR="$HOME/.local/share/applications"
ICON_DIR="$HOME/.local/share/icons/hicolor/64x64/apps"
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
Categories=Utility;Accessibility;
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

mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR" "$INSTALL_DATA_DIR/models"
install -m 0755 "$BIN_SRC" "$BIN_DEST"
# Use the colored app-icon master (British Shorthair) for the .desktop
# launcher entry. The monochrome tray icons live alongside the binary
# (embedded in cmd/murrly/assets/tray/*.png).
install -m 0644 "$REPO_ROOT/assets/icons/masters/app_icon_master_1024.png" "$ICON_DIR/$APP_ID.png"

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

if command -v gtk-update-icon-cache >/dev/null 2>&1; then
	gtk-update-icon-cache "$HOME/.local/share/icons/hicolor" >/dev/null 2>&1 || true
fi

cat <<EOF
Installed:
  Binary:  $BIN_DEST
  Launcher: $APP_DIR/$APP_ID.desktop
  Model dir: $INSTALL_DATA_DIR/models

Start it from the application menu or run:
  $BIN_DEST
EOF
