//go:build darwin

package paster

import "os/exec"

// Paste sends Cmd+V to the focused application via osascript.
// Requires the calling .app bundle to be granted Accessibility permission.
// See internal/macospermissions.EnsureAccessibility.
func (p *Paster) Paste() error {
	return exec.Command(
		"osascript", "-e",
		`tell application "System Events" to keystroke "v" using {command down}`,
	).Run()
}
