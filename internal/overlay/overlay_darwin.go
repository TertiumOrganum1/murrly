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

// Show displays the overlay pill with the given icon (PNG bytes) and
// label text. icon may be nil — then the label is centered without an
// icon. The PNG is rendered as a template image tinted white so it sits
// well on the dark pill.
func Show(icon []byte, text string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	var iconPtr *C.uchar
	var iconLen C.size_t
	if len(icon) > 0 {
		iconPtr = (*C.uchar)(unsafe.Pointer(&icon[0]))
		iconLen = C.size_t(len(icon))
	}
	C.mur_overlay_show(iconPtr, iconLen, c)
}

// Hide tears down the overlay pill.
func Hide() {
	C.mur_overlay_hide()
}
