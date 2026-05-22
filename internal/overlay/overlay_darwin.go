//go:build darwin

// Package overlay shows a small translucent status pill at the top of the
// screen so the user can see Murrly's state at a glance — useful when the
// tray icon is hidden behind the notch on M-series Macs.
//
// All Cocoa work is dispatched onto the main thread via
// dispatch_async(dispatch_get_main_queue(), ...), so Show/Hide can be
// called safely from any goroutine.
package overlay

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework QuartzCore

#include <stdlib.h>
#include "overlay.h"
*/
import "C"

import "unsafe"

// Show displays the overlay pill with the given label text.
func Show(text string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.mur_overlay_show(c)
}

// Hide tears down the overlay pill.
func Hide() {
	C.mur_overlay_hide()
}
