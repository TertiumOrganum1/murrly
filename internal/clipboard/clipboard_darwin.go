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
	"os/exec"
	"unsafe"
)

// readSystemSnapshot is the macOS half of Snapshot (see clipboard.go, which
// filters out our own dictations before the caller sees them). Shells out to
// pbpaste rather than growing the native shim: it is one read, on the
// recording path where nothing waits for it, and a failure simply means no
// snapshot.
//
// Text only here. Keeping a picture is a Linux answer to a Linux problem —
// there a dictation destroys the clipboard's image because our xclip takes the
// selection away from whoever held it, while NSPasteboard is a store and the
// image survives the write of a new item.
func (c *Clipboard) readSystemSnapshot() (Saved, bool) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil || len(out) == 0 {
		return Saved{}, false
	}
	return Saved{Text: string(out), HasContent: true}, true
}

// Publish writes the text and hands back a no-op release: NSPasteboard is a
// server-side store, so nothing of ours keeps owning it afterwards. The
// release func exists for the X11 implementation's sake (see
// clipboard_linux.go).
func (c *Clipboard) Publish(text string) (func(), error) {
	if err := c.writeText(text); err != nil {
		return func() {}, err
	}
	markOurs(text)
	return func() {}, nil
}

// PublishAndHold is Publish: there is nothing to hold onto. NSPasteboard keeps
// the text on its own, so the distinction the X11 implementation draws between
// publishing and holding does not exist here.
func (c *Clipboard) PublishAndHold(text string) error {
	if err := c.writeText(text); err != nil {
		return err
	}
	markOurs(text)
	return nil
}

// Set replaces the pasteboard with a single UTF-8 plain text item at the
// user's request, so the text counts as theirs — see forgetOurs.
func (c *Clipboard) Set(text string) error {
	if err := c.writeText(text); err != nil {
		return err
	}
	forgetOurs()
	return nil
}

// writeText is the raw pasteboard write shared by Publish and Set.
// Atomic — no torn intermediate state visible to other apps.
func (c *Clipboard) writeText(text string) error {
	ctext := C.CString(text)
	defer C.free(unsafe.Pointer(ctext))
	if rc := C.mur_clip_write_text(ctext); rc != 0 {
		return fmt.Errorf("clipboard: NSPasteboard write failed (rc=%d)", int(rc))
	}
	return nil
}

// Release is a no-op on macOS: NSPasteboard is a server-side store, so
// nothing of ours keeps owning it after a write.
func (c *Clipboard) Release() {}
