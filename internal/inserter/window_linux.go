//go:build linux

package inserter

import (
	"os/exec"
	"strings"
)

// activeWindowID returns the focused X11 window id, or "?" when it cannot
// be read. Diagnostic only: it tells whether the clipboard was published
// into a window that was not the one the paste later landed in.
func activeWindowID() string {
	out, err := exec.Command("xdotool", "getactivewindow", "getwindowname").Output()
	if err != nil {
		return "?"
	}
	fields := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	return strings.Join(fields, " | ")
}
