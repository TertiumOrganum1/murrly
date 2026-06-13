//go:build windows

package clipboard

import (
	"fmt"
	"log"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows clipboard model: a single global clipboard (no X11-style primary
// selection), accessed through Open/Close with the calling thread holding it
// for the duration. Save snapshots the most-useful current payload — an image
// (CF_DIB) if one is present (the screenshot-then-dictate case), otherwise
// Unicode text — so it survives the Set→Ctrl+V→Restore dictation cycle. Other
// formats (files, RTF) aren't round-tripped; HasContent stays false for them
// and Restore clears, matching the Linux "best effort, keep dictating" stance.

const (
	cfUnicodeText = 13
	cfDIB         = 8

	gmemMoveable = 0x0002

	// dibTarget marks a Saved snapshot as a device-independent bitmap so
	// Restore re-publishes it under CF_DIB rather than as text.
	dibTarget = "CF_DIB"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procIsFormatAvail    = user32.NewProc("IsClipboardFormatAvailable")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
)

// openClipboard tries a few times — another app (or our own previous paste)
// may briefly hold it. Returns false if it never opens; callers degrade to a
// no-op rather than blocking the insert.
func openClipboard() bool {
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func closeClipboard() { procCloseClipboard.Call() }

func isFormatAvailable(format uintptr) bool {
	r, _, _ := procIsFormatAvail.Call(format)
	return r != 0
}

// readFormat copies the raw bytes of a clipboard format out of the global
// memory the clipboard owns. Returns nil if the format is empty/unlockable.
func readFormat(format uintptr) []byte {
	h, _, _ := procGetClipboardData.Call(format)
	if h == 0 {
		return nil
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return nil
	}
	out := make([]byte, int(size))
	// ptr addresses the clipboard's global block (OS memory, not Go-managed),
	// so the uintptr→Pointer conversion is safe despite go vet's generic note.
	copy(out, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size)))
	return out
}

// writeFormat publishes raw bytes under a clipboard format. The clipboard must
// already be open and emptied. On success the OS takes ownership of the global
// block (we must not free it).
func writeFormat(format uintptr, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc failed")
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return fmt.Errorf("clipboard: GlobalLock failed")
	}
	// ptr is OS-owned global memory, not Go-managed — uintptr→Pointer is safe.
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(data)), data)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		return fmt.Errorf("clipboard: SetClipboardData failed: %v", err)
	}
	return nil
}

func (c *Clipboard) Save() (Saved, error) {
	if !openClipboard() {
		log.Printf("clipboard: could not open for save; skipping snapshot")
		return Saved{}, nil
	}
	defer closeClipboard()

	// Prefer the image: a copied screenshot is the payload most worth
	// preserving across a dictation paste.
	if isFormatAvailable(cfDIB) {
		if data := readFormat(cfDIB); len(data) > 0 {
			return Saved{HasContent: true, Binary: data, Target: dibTarget}, nil
		}
	}
	if isFormatAvailable(cfUnicodeText) {
		if data := readFormat(cfUnicodeText); len(data) > 0 {
			u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2)
			return Saved{HasContent: true, Text: windows.UTF16ToString(u16)}, nil
		}
	}
	// Some other (unhandled) format, or empty clipboard. HasContent stays
	// false so Restore clears rather than re-publishing garbage.
	return Saved{}, nil
}

func (c *Clipboard) Set(text string) error {
	if !openClipboard() {
		return fmt.Errorf("clipboard: could not open to set text")
	}
	defer closeClipboard()
	procEmptyClipboard.Call()
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	return writeFormat(cfUnicodeText, unsafe.Slice((*byte)(unsafe.Pointer(&u16[0])), len(u16)*2))
}

func (c *Clipboard) Restore(s Saved) error {
	if !openClipboard() {
		return fmt.Errorf("clipboard: could not open to restore")
	}
	defer closeClipboard()
	procEmptyClipboard.Call()

	switch {
	case !s.HasContent:
		// Already emptied above — nothing to put back.
		return nil
	case s.Target == dibTarget && len(s.Binary) > 0:
		return writeFormat(cfDIB, s.Binary)
	default:
		u16, err := windows.UTF16FromString(s.Text)
		if err != nil {
			return err
		}
		return writeFormat(cfUnicodeText, unsafe.Slice((*byte)(unsafe.Pointer(&u16[0])), len(u16)*2))
	}
}
