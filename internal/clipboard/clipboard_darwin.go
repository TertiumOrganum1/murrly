//go:build darwin

package clipboard

/*
#cgo darwin LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include "clipboard_darwin.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Save snapshots the entire NSPasteboard via the native API. Every type
// of every item (text, image, RTF, file URLs, …) is captured into an
// opaque token carried through Restore — so a copied screenshot survives
// a transcription paste exactly as it was.
//
// macOS has no separate "primary" selection — Primary / HasPrimary stay
// zero.
func (c *Clipboard) Save() (Saved, error) {
	token := C.mur_clip_save_state()
	if token == nil {
		return Saved{}, nil
	}
	return Saved{
		HasContent:    true,
		platformState: uintptr(token),
	}, nil
}

// Set replaces the pasteboard with a single UTF-8 plain text item.
// Atomic — no torn intermediate state visible to other apps.
func (c *Clipboard) Set(text string) error {
	ctext := C.CString(text)
	defer C.free(unsafe.Pointer(ctext))
	if rc := C.mur_clip_write_text(ctext); rc != 0 {
		return fmt.Errorf("clipboard: NSPasteboard write failed (rc=%d)", int(rc))
	}
	return nil
}

// Restore reinstates the pasteboard from a Save() snapshot. Consumes
// (frees) the platform-state token. If the snapshot was empty, the
// pasteboard is cleared. RestorePrimary is a no-op on macOS.
func (c *Clipboard) Restore(s Saved) error {
	if s.platformState == 0 {
		C.mur_clip_restore_state(nil)
		return nil
	}
	C.mur_clip_restore_state(unsafe.Pointer(s.platformState))
	return nil
}
