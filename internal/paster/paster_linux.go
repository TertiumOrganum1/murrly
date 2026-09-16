//go:build linux

package paster

import (
	"encoding/binary"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

var capsLockOnRe = regexp.MustCompile(`(?i)Caps Lock:\s*on`)

// pasteSettleDelay waits for the push-to-talk key (F12 / Break) to finish
// releasing before the synthetic Ctrl+V. Without it the modifier race —
// the physical key still going up while we press Ctrl — intermittently
// dropped the Ctrl and typed a literal "v" instead of pasting.
//
// It is a window measured from the key release, not a flat sleep — see
// settleRemaining in paster.go. On a dictation it has always fully elapsed
// during transcription, so the insert pays nothing; raise the window if a
// literal "v" ever reappears on the paths that do paste immediately.
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
	time.Sleep(settleRemaining(pasteSettleDelay))
	shift := focusIsTerminal()
	capsBefore := capsLockOn()
	// Last moment before the target application can fetch the clipboard.
	if beforeKey != nil {
		beforeKey()
	}
	mods := "ctrl"
	if shift {
		mods = "ctrl+shift"
	}
	if err := exec.Command("xdotool", "keydown", "--clearmodifiers", mods).Run(); err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	_ = exec.Command("xdotool", "key", "--delay", "30", "v").Run()
	err := exec.Command("xdotool", "keyup", mods).Run()
	if capsBefore != capsLockOn() {
		_ = exec.Command("xdotool", "key", "Caps_Lock").Run()
	}
	return err
}

// terminalClasses are the window classes where Ctrl+V is not paste.
//
// In a terminal Ctrl+V is the readline/VTE "quoted insert": it takes the next
// keystroke literally, so the synthetic chord puts a visible ^V in the line and
// swallows the v. Those emulators paste on Ctrl+Shift+V instead. The list is by
// window class rather than by heuristic because the exceptions matter both
// ways: VS Code's integrated terminal lives in a window of class "code", where
// Ctrl+V is an ordinary paste, and adding Shift there would open the markdown
// preview instead.
var terminalClasses = map[string]bool{
	"gnome-terminal":        true,
	"gnome-terminal-server": true,
	"mate-terminal":         true,
	"xfce4-terminal":        true,
	"konsole":               true,
	"qterminal":             true,
	"terminator":            true,
	"tilix":                 true,
	"guake":                 true,
	"alacritty":             true,
	"kitty":                 true,
	"wezterm":               true,
	"foot":                  true,
	"st":                    true,
	"xterm":                 true,
	"uxterm":                true,
	"urxvt":                 true,
	"rxvt":                  true,
	"termite":               true,
	"sakura":                true,
	"lxterminal":            true,
	"deepin-terminal":       true,
	"x-terminal-emulator":   true,
}

// focusIsTerminal reports whether the window about to receive the chord is a
// terminal emulator. Any failure answers false, which keeps the plain Ctrl+V.
//
// This used to shell out to `xdotool getactivewindow getwindowclassname`, and
// that silently never worked: getwindowclassname does not exist in xdotool
// 3.20160805.1, which is what Debian and Ubuntu still ship. The command failed
// every time, the error became "not a terminal", and every paste into a
// terminal put a literal ^V on the line. Reading the property ourselves costs
// no fork and cannot go stale against somebody's xdotool version.
//
// _NET_ACTIVE_WINDOW rather than the input focus on purpose: it names the
// top-level window the window manager considers active, which is the one
// carrying WM_CLASS. The input focus is often a child that has no class of
// its own.
func focusIsTerminal() bool {
	instance, class, ok := activeWindowClass()
	if !ok {
		return false
	}
	return isTerminalClass(instance, class)
}

// isTerminalClass decides on an already-read WM_CLASS pair. Both halves count:
// konsole reports "x-terminal-emulator" as its instance and "konsole" as its
// class, and which of the two names an emulator by varies across them.
func isTerminalClass(instance, class string) bool {
	return terminalClasses[strings.ToLower(instance)] || terminalClasses[strings.ToLower(class)]
}

// x11 is the connection focusIsTerminal reads through, opened once and kept for
// the life of the process. Failing to connect is not fatal anywhere: every
// caller treats "unknown" as "not a terminal".
var (
	x11Once sync.Once
	x11Conn *xgb.Conn
	x11Root xproto.Window
	x11Prop xproto.Atom
)

func x11() (*xgb.Conn, bool) {
	x11Once.Do(func() {
		conn, err := xgb.NewConn()
		if err != nil {
			return
		}
		setup := xproto.Setup(conn)
		if setup == nil || len(setup.Roots) == 0 {
			conn.Close()
			return
		}
		name := "_NET_ACTIVE_WINDOW"
		atom, err := xproto.InternAtom(conn, true, uint16(len(name)), name).Reply()
		if err != nil || atom == nil {
			conn.Close()
			return
		}
		x11Conn, x11Root, x11Prop = conn, setup.DefaultScreen(conn).Root, atom.Atom
	})
	return x11Conn, x11Conn != nil
}

// activeWindowClass returns the WM_CLASS of the active window as its
// (instance, class) pair — konsole, for one, reports the interesting half in
// the instance ("x-terminal-emulator") and its own name in the class.
func activeWindowClass() (instance, class string, ok bool) {
	conn, ok := x11()
	if !ok {
		return "", "", false
	}
	active, err := xproto.GetProperty(conn, false, x11Root, x11Prop,
		xproto.AtomWindow, 0, 1).Reply()
	if err != nil || active == nil || len(active.Value) < 4 {
		return "", "", false
	}
	win := xproto.Window(binary.LittleEndian.Uint32(active.Value))
	if win == 0 {
		return "", "", false
	}
	// WM_CLASS is two NUL-terminated strings, instance first. 256 longs is
	// far more than any real class pair and costs one round trip either way.
	prop, err := xproto.GetProperty(conn, false, win, xproto.AtomWmClass,
		xproto.AtomString, 0, 256).Reply()
	if err != nil || prop == nil || len(prop.Value) == 0 {
		return "", "", false
	}
	instance, class = parseWMClass(prop.Value)
	return instance, class, true
}

// parseWMClass splits the raw WM_CLASS property into its (instance, class)
// pair. The property is two NUL-terminated strings back to back; a window that
// has set only one of them, or neither, must not make this panic.
func parseWMClass(raw []byte) (instance, class string) {
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(parts) > 0 {
		instance = parts[0]
	}
	if len(parts) > 1 {
		class = parts[1]
	}
	return instance, class
}

// ReleaseModifiers forces every modifier up before a synthetic chord.
//
// It exists for the paste-last binding: that one fires on the key PRESS of
// Shift+F12, so the user's Shift is still physically down a moment later when
// the Ctrl+V goes out — and the target sees Ctrl+Shift+V, which is a
// different command in plenty of applications (markdown preview in VS Code,
// paste-without-formatting elsewhere). --clearmodifiers on the Ctrl keydown
// is not enough: it restores the modifiers afterwards, i.e. before the V.
// The dictation paths don't need this — seconds of transcription pass between
// their key press and the chord.
//
// Releasing a key the user is still holding is harmless: X sends another
// KeyRelease when they let go, and nothing tracks the physical state.
func (p *Paster) ReleaseModifiers() {
	_ = exec.Command("xdotool", "keyup",
		"Control_L", "Control_R", "Shift_L", "Shift_R",
		"Alt_L", "Alt_R", "Super_L", "Super_R").Run()
}

func capsLockOn() bool {
	out, err := exec.Command("xset", "q").Output()
	if err != nil {
		return false
	}
	return capsLockOnRe.Match(out)
}
