//go:build windows

package uicontext

/*
#cgo windows CXXFLAGS: -std=c++11
#cgo windows LDFLAGS: -lole32 -loleaut32 -lstdc++
#include "uicontext_windows.h"
*/
import "C"

import "fmt"

// stageDesc maps the C capture stage to a human label for the log.
var stageDesc = map[int]string{
	1: "COM/CoCreateInstance failed",
	2: "no focused UI element",
	3: "focused control has no TextPattern (opaque/rich editor, terminal)",
	4: "QueryInterface(TextPattern) failed",
	5: "GetSelection failed",
	6: "empty selection / no caret range",
}

// Capture reads the focused control's text around the caret via UI Automation
// (the Windows analogue of AT-SPI on Linux). It runs everywhere a TextPattern
// is available — native Win32 fields AND Electron/Chromium (VS Code, chat
// inputs). The one tricky case, an empty field that reports placeholder text
// as content, is handled inside the C capture (looksEmptyOrPlaceholder), which
// makes such a field read as a fresh start rather than a mid-sentence insert.
// On any failure HasContext stays false and Apply passes the dictation through
// unchanged.
func Capture() Context {
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
