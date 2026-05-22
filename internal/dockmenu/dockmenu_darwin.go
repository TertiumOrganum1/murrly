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
extern void murGoToggleAutostart(void);
extern void murGoOpenConfig(void);
extern void murGoQuit(void);
*/
import "C"

import (
	"sync"
	"unsafe"
)

var (
	cbMu              sync.Mutex
	cbCopy            func(int)
	cbToggleAutostart func()
	cbOpenConfig      func()
	cbQuit            func()
)

// Install registers callbacks for the Dock menu actions. Must be called
// after NSApp exists.
func Install(onCopy func(int), onToggleAutostart, onOpenConfig, onQuit func()) {
	cbMu.Lock()
	cbCopy = onCopy
	cbToggleAutostart = onToggleAutostart
	cbOpenConfig = onOpenConfig
	cbQuit = onQuit
	cbMu.Unlock()
	C.mur_dockmenu_install(
		(*[0]byte)(C.murGoCopy),
		(*[0]byte)(C.murGoToggleAutostart),
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

// SetAutostart updates the checkmark next to the "Запускать при логине"
// item to reflect the current Login Item state.
func SetAutostart(enabled bool) {
	v := C.int(0)
	if enabled {
		v = 1
	}
	C.mur_dockmenu_set_autostart(v)
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

//export murGoToggleAutostart
func murGoToggleAutostart() {
	cbMu.Lock()
	cb := cbToggleAutostart
	cbMu.Unlock()
	if cb != nil {
		cb()
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
