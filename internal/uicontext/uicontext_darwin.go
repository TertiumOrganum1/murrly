//go:build darwin

package uicontext

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework ApplicationServices -framework CoreFoundation
#include "uicontext_darwin.h"
*/
import "C"

// captureStatus maps the C-side mur_focus_status_t enum to a
// human-readable string for logging.
var captureStatus = map[int]string{
	0: "no-systemwide",
	1: "no-focused",
	2: "no-range",
	3: "at-start",
	4: "value-ok",
	5: "param-fallback-ok",
	6: "no-value",
}

// Capture reads the focused UI element via the macOS Accessibility
// API and returns what's immediately to the left of the cursor.
//
// Safe to call from any goroutine. Cost is one or two AX round-trips
// (parameterized fallback only fires when kAXValueAttribute is
// blocked) — usually sub-millisecond, bounded by the focused app's
// responsiveness.
//
// HasContext == false means the AX path didn't yield actionable
// info; Apply then passes the text through unchanged. The Status
// field is for diagnostics — see captureStatus for the mapping.
func Capture() Context {
	c := C.mur_read_focus_context()
	status, ok := captureStatus[int(c.status)]
	if !ok {
		status = "unknown"
	}
	return Context{
		HasContext: c.ok == 1,
		AtStart:    c.at_start == 1,
		Preceding:  rune(c.preceding),
		Status:     status,
	}
}
