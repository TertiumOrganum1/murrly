//go:build darwin

// Package dockmenu installs a right-click menu on the Dock icon.
package dockmenu

/*
#cgo darwin LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include "dockmenu.h"

extern void murGoCopy(int index);
extern void murGoPickModel(int index);
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
	cbPickModel       func(int)
	cbToggleAutostart func()
	cbOpenConfig      func()
	cbQuit            func()
)

// Install registers callbacks for the Dock menu actions and seeds the
// Model submenu with the provided labels.
func Install(
	onCopy func(int),
	onPickModel func(int),
	onToggleAutostart, onOpenConfig, onQuit func(),
	modelLabels []string,
) {
	cbMu.Lock()
	cbCopy = onCopy
	cbPickModel = onPickModel
	cbToggleAutostart = onToggleAutostart
	cbOpenConfig = onOpenConfig
	cbQuit = onQuit
	cbMu.Unlock()

	cLabels := make([]*C.char, len(modelLabels))
	for i, s := range modelLabels {
		cLabels[i] = C.CString(s)
	}
	defer func() {
		for _, p := range cLabels {
			C.free(unsafe.Pointer(p))
		}
	}()
	var labelsPtr **C.char
	if len(cLabels) > 0 {
		labelsPtr = (**C.char)(unsafe.Pointer(&cLabels[0]))
	}

	C.mur_dockmenu_install(
		(*[0]byte)(C.murGoCopy),
		(*[0]byte)(C.murGoPickModel),
		(*[0]byte)(C.murGoToggleAutostart),
		(*[0]byte)(C.murGoOpenConfig),
		(*[0]byte)(C.murGoQuit),
		labelsPtr,
		C.int(len(cLabels)),
	)
}

func SetTranscripts(latest, previous, older string) {
	cLatest := C.CString(latest)
	cPrev := C.CString(previous)
	cOlder := C.CString(older)
	defer C.free(unsafe.Pointer(cLatest))
	defer C.free(unsafe.Pointer(cPrev))
	defer C.free(unsafe.Pointer(cOlder))
	C.mur_dockmenu_set_transcripts(cLatest, cPrev, cOlder)
}

func SetAutostart(enabled bool) {
	v := C.int(0)
	if enabled {
		v = 1
	}
	C.mur_dockmenu_set_autostart(v)
}

// SetActiveModel marks the model at the given index (matching the order
// passed to Install) with a checkmark, clearing others. Pass -1 to clear.
func SetActiveModel(index int) {
	C.mur_dockmenu_set_model_index(C.int(index))
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

//export murGoPickModel
func murGoPickModel(index C.int) {
	cbMu.Lock()
	cb := cbPickModel
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
