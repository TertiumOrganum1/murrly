//go:build darwin

// Package autostart toggles "launch at login" for Murrly. On macOS the
// state lives in System Events Login Items; we drive it via osascript so
// the same code path matches what scripts/install-mac.sh --autostart does.
package autostart

import (
	"fmt"
	"os/exec"
	"strings"
)

const appName = "Murrly"
const appPath = "/Applications/Murrly.app"

// Enabled reports whether Murrly is in the user's Login Items list.
func Enabled() bool {
	out, err := exec.Command(
		"osascript", "-e",
		fmt.Sprintf(`tell application "System Events" to exists login item "%s"`, appName),
	).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// Enable adds Murrly to Login Items. Idempotent — if already there, a
// no-op (osascript would error; we swallow that).
func Enable() error {
	if Enabled() {
		return nil
	}
	return exec.Command(
		"osascript", "-e",
		fmt.Sprintf(
			`tell application "System Events" to make login item at end with properties {path:"%s", hidden:true}`,
			appPath,
		),
	).Run()
}

// Disable removes Murrly from Login Items. Idempotent.
func Disable() error {
	if !Enabled() {
		return nil
	}
	return exec.Command(
		"osascript", "-e",
		fmt.Sprintf(`tell application "System Events" to delete login item "%s"`, appName),
	).Run()
}
