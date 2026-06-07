//go:build linux

package paster

import (
	"os/exec"
	"regexp"
	"time"
)

var capsLockOnRe = regexp.MustCompile(`(?i)Caps Lock:\s*on`)

// pasteSettleDelay waits for the push-to-talk key (F12 / Break) to finish
// releasing before the synthetic Ctrl+V. Without it the modifier race —
// the physical key still going up while we press Ctrl — intermittently
// dropped the Ctrl and typed a literal "v" instead of pasting. ~1/3 s is
// generous; lower it if the insert feels laggy.
const pasteSettleDelay = 300 * time.Millisecond

// Paste sends Ctrl+V to the currently focused window via xdotool. The
// modifier is held explicitly (keydown ctrl → key v → keyup ctrl) rather
// than via a single `key ctrl+v`, so Ctrl is guaranteed down for the v —
// the one-shot chord sometimes lost the modifier and pasted nothing (just
// "v"). --clearmodifiers on the keydown releases the user's own held
// modifiers first. CapsLock handling: xdotool under --clearmodifiers can
// flip CapsLock off and fail to restore it, so snapshot via `xset q` and
// replay a Caps_Lock toggle if it ended up flipped.
func (p *Paster) Paste() error {
	time.Sleep(pasteSettleDelay)
	capsBefore := capsLockOn()
	if err := exec.Command("xdotool", "keydown", "--clearmodifiers", "ctrl").Run(); err != nil {
		return err
	}
	_ = exec.Command("xdotool", "key", "--delay", "30", "v").Run()
	err := exec.Command("xdotool", "keyup", "ctrl").Run()
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
