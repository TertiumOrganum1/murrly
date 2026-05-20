// Package paster sends Ctrl+V to the focused X11 window via xdotool.
package paster

import "os/exec"

type Paster struct{}

func New() *Paster { return &Paster{} }

// Paste sends Ctrl+V to the currently focused window.
// --clearmodifiers releases any modifier the user might still be holding
// (e.g. the hotkey itself) before sending the synthetic chord.
func (p *Paster) Paste() error {
	return exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+v").Run()
}
