//go:build windows

package clipboard

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows clipboard model: a single global clipboard (no X11-style primary
// selection), accessed through Open/Close with the calling thread holding it
// for the duration. There is no owning process to serve pastes, so unlike X11
// nothing of ours stays alive after a write — see clipboard_linux.go for the
// side of this package where that matters.

const (
	cfUnicodeText = 13

	gmemMoveable = 0x0002
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

// readSystemSnapshot is the Win32 half of Snapshot (see clipboard.go, which
// filters out our own dictations before the caller sees them).
//
// Text only here. Keeping a picture is a Linux answer to a Linux problem —
// there a dictation destroys the clipboard's image because our xclip takes the
// selection away from whoever held it, while the Win32 clipboard is a store
// and CF_BITMAP survives alongside our text write.
func (c *Clipboard) readSystemSnapshot() (Saved, bool) {
	if !openClipboard() {
		return Saved{}, false
	}
	defer closeClipboard()
	if !isFormatAvailable(cfUnicodeText) {
		return Saved{}, false
	}
	data := readFormat(cfUnicodeText)
	if len(data) == 0 {
		return Saved{}, false
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2)
	return Saved{HasContent: true, Text: windows.UTF16ToString(u16)}, true
}

// Publish writes the text and hands back a no-op release: the Win32 clipboard
// is a system-owned store. The release func exists for the X11
// implementation's sake (see clipboard_linux.go).
func (c *Clipboard) Publish(text string) (func(), error) {
	if err := c.writeText(text); err != nil {
		return func() {}, err
	}
	markOurs(text)
	return func() {}, nil
}

// PublishAndHold is Publish: there is nothing to hold onto. The Win32
// clipboard keeps the text itself, so the distinction the X11 implementation
// draws between publishing and holding does not exist here.
func (c *Clipboard) PublishAndHold(text string) error {
	if err := c.writeText(text); err != nil {
		return err
	}
	markOurs(text)
	return nil
}

// Set writes text at the user's request, so it counts as theirs from then on
// — see forgetOurs.
func (c *Clipboard) Set(text string) error {
	if err := c.writeText(text); err != nil {
		return err
	}
	forgetOurs()
	return nil
}

// writeText is the raw clipboard write shared by Publish and Set.
func (c *Clipboard) writeText(text string) error {
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

// Release is a no-op on Windows: the clipboard is a system-owned store and
// our writes do not leave a process serving it.
func (c *Clipboard) Release() {}
