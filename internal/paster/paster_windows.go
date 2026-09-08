//go:build windows

package paster

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pasteSettleDelay waits for the push-to-talk key (F12) to finish releasing
// before the synthetic Ctrl+V. Without it the modifier race — the physical
// key still going up while we press Ctrl — can drop the Ctrl and type a
// literal "v" instead of pasting. Mirrors the Linux paster's settle delay.
const pasteSettleDelay = 300 * time.Millisecond

// pasteConsumeDelay holds AFTER Ctrl+V so the target window actually reads the
// clipboard before the caller restores the previous contents. On Windows the
// clipboard is a one-shot copy (unlike X11, where the owner serves paste
// requests on demand), so restoring too early makes a slow app — especially
// Electron/Chromium — paste the OLD clipboard instead of the dictation. This
// blocks Paste() until the read has almost certainly happened.
const pasteConsumeDelay = 400 * time.Millisecond

const (
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
	vkShift        = 0x10
	vkControl      = 0x11
	vkMenu         = 0x12 // Alt
	vkLWin         = 0x5B
	vkRWin         = 0x5C
	vkV            = 0x56
)

// keybdInput mirrors Win32 KEYBDINPUT.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input mirrors Win32 INPUT (keyboard variant). The trailing pad makes the
// struct match the size of the MOUSEINPUT union member on amd64 so cbSize
// (sizeof INPUT) is correct for SendInput.
type input struct {
	inputType uint32
	ki        keybdInput
	_         [8]byte
}

var (
	user32      = windows.NewLazySystemDLL("user32.dll")
	procSendInp = user32.NewProc("SendInput")
)

// ReleaseModifiers forces every modifier up before a synthetic chord. See the
// Linux implementation for why: the paste-last binding fires on the key press
// of Shift+F12, so without this the Ctrl+V that follows arrives as
// Ctrl+Shift+V.
func (p *Paster) ReleaseModifiers() {
	events := []input{
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkShift, dwFlags: keyeventfKeyUp}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkControl, dwFlags: keyeventfKeyUp}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkMenu, dwFlags: keyeventfKeyUp}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkLWin, dwFlags: keyeventfKeyUp}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkRWin, dwFlags: keyeventfKeyUp}},
	}
	procSendInp.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]),
	)
}

// Paste synthesises Ctrl+V to the focused window via SendInput. Ctrl is held
// down explicitly around the V (down ctrl → down v → up v → up ctrl) so the
// modifier is guaranteed present for the keypress.
func (p *Paster) Paste(beforeKey func()) error {
	time.Sleep(pasteSettleDelay)
	beforeKey()

	events := []input{
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkControl}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkV}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkV, dwFlags: keyeventfKeyUp}},
		{inputType: inputKeyboard, ki: keybdInput{wVk: vkControl, dwFlags: keyeventfKeyUp}},
	}
	n, _, err := procSendInp.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]),
	)
	if int(n) != len(events) {
		return fmt.Errorf("paster: SendInput injected %d/%d events: %v", n, len(events), err)
	}
	// Let the target consume the paste before the caller restores the old
	// clipboard, so a slow app doesn't paste the previous contents.
	time.Sleep(pasteConsumeDelay)
	return nil
}
