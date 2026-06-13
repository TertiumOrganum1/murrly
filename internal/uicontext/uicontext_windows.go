//go:build windows

package uicontext

/*
#cgo windows CXXFLAGS: -std=c++11
#cgo windows LDFLAGS: -lole32 -loleaut32 -lstdc++
#include "uicontext_windows.h"
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// stageDesc maps the C capture stage to a human label for the log.
var stageDesc = map[int]string{
	1: "COM/CoCreateInstance failed",
	2: "no focused UI element",
	3: "focused control has no TextPattern (opaque/rich editor, terminal)",
	4: "QueryInterface(TextPattern) failed",
	5: "GetSelection failed",
	6: "empty selection / no caret range",
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWin = user32.NewProc("GetForegroundWindow")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
)

// foregroundClass returns the window class of the foreground top-level window.
func foregroundClass() string {
	h, _, _ := procGetForegroundWin.Call()
	if h == 0 {
		return ""
	}
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// Capture reads the focused control's text around the caret via UI Automation
// (the Windows analogue of AT-SPI on Linux). On any failure — no focus, a
// control without a TextPattern (terminals, custom editors) — HasContext stays
// false and Apply passes the dictation through unchanged.
//
// Electron/Chromium windows are deliberately skipped: their editable fields
// report placeholder text as real content with a pinned caret (the VS Code /
// Claude Code chat input), so UIA can't tell an empty field from a
// mid-sentence one. Rather than mangle the dictation (leading space, lower-
// cased first letter, dropped final punctuation), we pass the text through
// unchanged there — matching the Linux behaviour of bailing on opaque rich
// editors. Native Win32 fields (Notepad, WordPad, Office, native address
// bars) are unaffected.
func Capture() Context {
	if cls := foregroundClass(); strings.HasPrefix(cls, "Chrome_WidgetWin") {
		return Context{Status: "windows-uia: chromium/electron — pass-through"}
	}

	var c C.MurUICtx
	if C.mur_uictx_capture(&c) == 0 || c.hasContext == 0 {
		why := stageDesc[int(c.stage)]
		if why == "" {
			why = "no text focus"
		}
		return Context{Status: fmt.Sprintf("windows-uia: %s", why)}
	}
	return Context{
		HasContext:  true,
		AtStart:     c.atStart != 0,
		SpaceBefore: c.spaceBefore != 0,
		RightKnown:  c.rightKnown != 0,
		AtEnd:       c.atEnd != 0,
		Preceding:   rune(c.preceding),
		Following:   rune(c.following),
		Status:      "windows-uia",
	}
}
