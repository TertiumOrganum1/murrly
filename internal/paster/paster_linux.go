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
// dropped the Ctrl and typed a literal "v" instead of pasting.
//
// Was 300 ms, which is most of a third of a second added to every insert
// before anything happens. The race it guards against is over as soon as
// the key is up, and the transcription itself takes far longer than the
// user's finger — by the time we get here the key has been released for a
// while. Halved; raise it again if a literal "v" ever reappears.
const pasteSettleDelay = 150 * time.Millisecond

// Paste sends Ctrl+V to the currently focused window via xdotool. The
// modifier is held explicitly (keydown ctrl → key v → keyup ctrl) rather
// than via a single `key ctrl+v`, so Ctrl is guaranteed down for the v —
// the one-shot chord sometimes lost the modifier and pasted nothing (just
// "v"). --clearmodifiers on the keydown releases the user's own held
// modifiers first. CapsLock handling: xdotool under --clearmodifiers can
// flip CapsLock off and fail to restore it, so snapshot via `xset q` and
// replay a Caps_Lock toggle if it ended up flipped.
func (p *Paster) Paste(beforeKey func()) error {
	return p.PasteReady(beforeKey, nil)
}

// PasteReady is Paste with a second hook: ready runs with Ctrl already held
// down, immediately before V.
//
// That gap is where the clipboard actually gets read. Measured against a
// live Electron application, its final cache refresh lands ~50 ms before
// the V — after every delay we could put in front of the chord — and on the
// dictations where that refresh did NOT happen, the application pasted its
// stale cache instead of the dictation. Holding Ctrl and waiting for the
// read before releasing V turns that coin flip into a wait for an event we
// can actually see.
func (p *Paster) PasteReady(beforeKey func(), ready func()) error {
	time.Sleep(pasteSettleDelay)
	capsBefore := capsLockOn()
	// Last moment before the target application can fetch the clipboard.
	if beforeKey != nil {
		beforeKey()
	}
	if err := exec.Command("xdotool", "keydown", "--clearmodifiers", "ctrl").Run(); err != nil {
		return err
	}
	if ready != nil {
		ready()
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
