//go:build darwin

package paster

import (
	"os/exec"
	"testing"
)

// We cannot test Paste() end-to-end — that would require Accessibility
// permission and a focusable target app. Instead, verify osascript is
// available, which is what Paste relies on.
func TestOsascriptAvailable(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Fatalf("osascript not in PATH: %v", err)
	}
}
