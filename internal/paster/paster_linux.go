//go:build linux

package paster

import (
	"os/exec"
	"regexp"
)

var capsLockOnRe = regexp.MustCompile(`(?i)Caps Lock:\s*on`)

// Paste sends Ctrl+V to the currently focused window via xdotool.
// --clearmodifiers releases any held modifier so the user's Shift/Ctrl
// state doesn't garble the synthetic chord. But xdotool's handling of
// CapsLock under that flag is unreliable: it "releases" the toggle by
// sending CapsLock once (flipping state off) and intermittently fails
// to send the restoring press, leaving the user with CapsLock off
// after every paste. Snapshot via `xset q` around the call and replay
// a Caps_Lock toggle if state ended up flipped. NumLock doesn't hit
// this path — xdotool restores it reliably.
func (p *Paster) Paste() error {
	capsBefore := capsLockOn()
	err := exec.Command("xdotool", "key", "--clearmodifiers", "ctrl+v").Run()
	if capsBefore != capsLockOn() {
		_ = exec.Command("xdotool", "key", "Caps_Lock").Run()
	}
	return err
}

func capsLockOn() bool {
	out, err := exec.Command("xset", "q").Output()
	if err != nil {
		return false
	}
	return capsLockOnRe.Match(out)
}
