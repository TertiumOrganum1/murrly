//go:build darwin

// Package dockmenu installs a right-click menu on the Dock icon. Useful
// when the menu-bar tray icon is hidden behind the notch on M-series
// Macs — the Dock icon is always reachable.
package dockmenu

/*
#cgo darwin LDFLAGS: -framework Cocoa

#include "dockmenu.h"

extern void murGoQuit(void);
extern void murGoOpenConfig(void);
extern void murGoCopyLatest(void);
*/
import "C"

import "sync"

var (
	cbMu         sync.Mutex
	cbQuit       func()
	cbOpenConfig func()
	cbCopyLatest func()
)

// Install registers the three callbacks as Dock menu actions. Idempotent —
// calling more than once replaces the callbacks without re-attaching the
// menu. Must be called after NSApp exists (i.e. after systray.Run has
// started its NSApp loop, or any time later from a goroutine).
func Install(onQuit, onOpenConfig, onCopyLatest func()) {
	cbMu.Lock()
	cbQuit = onQuit
	cbOpenConfig = onOpenConfig
	cbCopyLatest = onCopyLatest
	cbMu.Unlock()
	C.mur_dockmenu_install(
		(*[0]byte)(C.murGoQuit),
		(*[0]byte)(C.murGoOpenConfig),
		(*[0]byte)(C.murGoCopyLatest),
	)
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

//export murGoOpenConfig
func murGoOpenConfig() {
	cbMu.Lock()
	cb := cbOpenConfig
	cbMu.Unlock()
	if cb != nil {
		cb()
	}
}

//export murGoCopyLatest
func murGoCopyLatest() {
	cbMu.Lock()
	cb := cbCopyLatest
	cbMu.Unlock()
	if cb != nil {
		cb()
	}
}
