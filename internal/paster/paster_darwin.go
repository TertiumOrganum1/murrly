//go:build darwin

package paster

/*
#cgo darwin LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// kVK_ANSI_V = 0x09 (US 'V' key — same physical key used for paste on all layouts).
static const CGKeyCode mur_kVK_V = 0x09;

// mur_paste_cmd_v synthesises a Cmd+V key chord at the HID event tap level.
// Requires the calling process to be granted Accessibility permission.
// Returns 0 on success, negative on failure.
static int mur_paste_cmd_v(void) {
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);
    if (src == NULL) return -1;

    CGEventRef down = CGEventCreateKeyboardEvent(src, mur_kVK_V, true);
    if (down == NULL) { CFRelease(src); return -2; }
    CGEventSetFlags(down, kCGEventFlagMaskCommand);

    CGEventRef up = CGEventCreateKeyboardEvent(src, mur_kVK_V, false);
    if (up == NULL) { CFRelease(down); CFRelease(src); return -3; }
    CGEventSetFlags(up, kCGEventFlagMaskCommand);

    CGEventPost(kCGHIDEventTap, down);
    CGEventPost(kCGHIDEventTap, up);

    CFRelease(down);
    CFRelease(up);
    CFRelease(src);
    return 0;
}
*/
import "C"

import "fmt"

// Paste synthesises Cmd+V via CGEventPost so it draws on Murrly's own
// Accessibility grant rather than a child osascript process which would
// require a separate (and surprising) permission entry.
func (p *Paster) Paste() error {
	if rc := C.mur_paste_cmd_v(); rc != 0 {
		return fmt.Errorf("paster: CGEventPost failed (rc=%d) — check Accessibility permission for Murrly in System Settings", int(rc))
	}
	return nil
}
