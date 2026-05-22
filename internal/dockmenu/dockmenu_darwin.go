//go:build darwin

// Package dockmenu installs a right-click menu on the Dock icon. Useful
// when the menu-bar tray icon is hidden behind the notch on M-series
// Macs — the Dock icon is always reachable.
package dockmenu

/*
#cgo darwin LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include "dockmenu.h"

extern void murGoCopy(int index);
extern void murGoOpenConfig(void);
extern void murGoQuit(void);
*/
import "C"

import (
	"sync"
	"unsafe"
)

var (
	cbMu         sync.Mutex
	cbCopy       func(int)
	cbOpenConfig func()
	cbQuit       func()
)

// Install registers callbacks for the Dock menu actions. Must be called
// after NSApp exists (any time during program lifetime is fine —
// internally we dispatch onto the main thread).
func Install(onCopy func(int), onOpenConfig, onQuit func()) {
	cbMu.Lock()
	cbCopy = onCopy
	cbOpenConfig = onOpenConfig
	cbQuit = onQuit
	cbMu.Unlock()
	C.mur_dockmenu_install(
		(*[0]byte)(C.murGoCopy),
		(*[0]byte)(C.murGoOpenConfig),
		(*[0]byte)(C.murGoQuit),
	)
}

// SetTranscripts updates the three Copy-transcript menu items to reflect
// the current history. Pass empty strings to disable a slot.
func SetTranscripts(latest, previous, older string) {
	cLatest := C.CString(latest)
	cPrev := C.CString(previous)
	cOlder := C.CString(older)
	defer C.free(unsafe.Pointer(cLatest))
	defer C.free(unsafe.Pointer(cPrev))
	defer C.free(unsafe.Pointer(cOlder))
	C.mur_dockmenu_set_transcripts(cLatest, cPrev, cOlder)
}

//export murGoCopy
func murGoCopy(index C.int) {
	cbMu.Lock()
	cb := cbCopy
	cbMu.Unlock()
	if cb != nil {
		cb(int(index))
	}
}

//export murGoOpenConfig
func murGoOpenConfig() {
	cbMu.Lock()
	cb := cbOpenConfig
	cbMu.Unlock()
	if cb != nil {
		cb()
	}
}

//export murGoQuit
func murGoQuit() {
	cbMu.Lock()
	cb := cbQuit
	cbMu.Unlock()
	if cb != nil {
		cb()
	}
}
