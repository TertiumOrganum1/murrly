//go:build linux

package paster

import "os/exec"

// Paste sends Ctrl+V to the currently focused window via xdotool.
// --clearmodifiers releases any modifier the user might still be holding
// (e.g. the hotkey itself) before sending the synthetic chord.
func (p *Paster) Paste() error {
	return exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+v").Run()
}
