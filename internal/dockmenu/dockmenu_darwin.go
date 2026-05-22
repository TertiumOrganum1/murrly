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
extern void murGoReloadConfig(void);
extern void murGoOpenMicSettings(void);
extern void murGoOpenAccessibility(void);
extern void murGoQuit(void);
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/tertiumorganum1/murrly/internal/menuactions"
)

var (
	actionsMu sync.RWMutex
	actions   *menuactions.Actions
)

// Install registers the menu actions and seeds the Model submenu with
// the labels provided in actions.ModelLabels. Safe to call once at app
// startup; the underlying NSApplicationDelegate chain is set up on the
// main thread asynchronously.
func Install(a *menuactions.Actions) {
	actionsMu.Lock()
	actions = a
	actionsMu.Unlock()

	cLabels := make([]*C.char, len(a.ModelLabels))
	for i, s := range a.ModelLabels {
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
		(*[0]byte)(C.murGoReloadConfig),
		(*[0]byte)(C.murGoOpenMicSettings),
		(*[0]byte)(C.murGoOpenAccessibility),
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
// passed in Actions.ModelLabels) with a checkmark, clearing others.
// Pass -1 to clear all.
func SetActiveModel(index int) {
	C.mur_dockmenu_set_model_index(C.int(index))
}

// loadActions is the read-side companion to the Install snapshot, used
// by every //export callback. RWMutex keeps callbacks lock-free for
// readers (Install only runs once at startup anyway).
func loadActions() *menuactions.Actions {
	actionsMu.RLock()
	a := actions
	actionsMu.RUnlock()
	return a
}

//export murGoCopy
func murGoCopy(index C.int) {
	if a := loadActions(); a != nil && a.OnCopyTranscript != nil {
		a.OnCopyTranscript(int(index))
	}
}

//export murGoPickModel
func murGoPickModel(index C.int) {
	if a := loadActions(); a != nil && a.OnPickModel != nil {
		a.OnPickModel(int(index))
	}
}

//export murGoToggleAutostart
func murGoToggleAutostart() {
	a := loadActions()
	if a == nil || a.OnToggleAutostart == nil {
		return
	}
	// Tray sets its own checkmark from the return value; the dock menu
	// pulls state via mur_dockmenu_set_autostart from the caller (main
	// wires this side after the toggle returns).
	a.OnToggleAutostart()
}

//export murGoOpenConfig
func murGoOpenConfig() {
	if a := loadActions(); a != nil && a.OnOpenConfig != nil {
		a.OnOpenConfig()
	}
}

//export murGoReloadConfig
func murGoReloadConfig() {
	if a := loadActions(); a != nil && a.OnReloadConfig != nil {
		a.OnReloadConfig()
	}
}

//export murGoOpenMicSettings
func murGoOpenMicSettings() {
	if a := loadActions(); a != nil && a.OnOpenMicSettings != nil {
		a.OnOpenMicSettings()
	}
}

//export murGoOpenAccessibility
func murGoOpenAccessibility() {
	if a := loadActions(); a != nil && a.OnOpenAccessibility != nil {
		a.OnOpenAccessibility()
	}
}

//export murGoQuit
func murGoQuit() {
	if a := loadActions(); a != nil && a.OnQuit != nil {
		a.OnQuit()
	}
}
