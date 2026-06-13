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

const (
	inputKeyboard   = 1
	keyeventfKeyUp  = 0x0002
	vkControl       = 0x11
	vkV             = 0x56
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

// Paste synthesises Ctrl+V to the focused window via SendInput. Ctrl is held
// down explicitly around the V (down ctrl → down v → up v → up ctrl) so the
// modifier is guaranteed present for the keypress.
func (p *Paster) Paste() error {
	time.Sleep(pasteSettleDelay)

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
	return nil
}
