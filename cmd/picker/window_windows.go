//go:build windows

package main

import (
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// arrangeWindow places the picker like a transient chooser on Windows:
// centred on the monitor under the mouse cursor, pinned top-most, and given
// foreground focus so Escape works. It then watches the foreground window and
// exits the moment focus leaves the picker — i.e. the user clicked anywhere
// else — so the window is always either visible-and-on-top or gone entirely
// (and the blocked parent's picker.Pick returns, freeing the next dictation).
//
// Run in a goroutine before ShowAndRun; it polls until the GLFW/Fyne window
// exists. All calls are best-effort — a failure just leaves Fyne's default
// placement in place.
func arrangeWindow() {
	var hwnd uintptr
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hwnd = ourWindow(); hwnd != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hwnd == 0 {
		return
	}
	// Let Fyne finish growing the window to its final (scaled) size before we
	// centre it, then re-fetch in case the HWND/size settled.
	time.Sleep(120 * time.Millisecond)
	if h := ourWindow(); h != 0 {
		hwnd = h
	}

	var wr rect
	getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	ww := wr.right - wr.left
	wh := wr.bottom - wr.top

	// Monitor under the mouse cursor → its work area (excludes the taskbar).
	var cur point
	getCursorPos.Call(uintptr(unsafe.Pointer(&cur)))
	pt := uintptr(uint32(cur.x)) | uintptr(uint32(cur.y))<<32
	mon, _, _ := monitorFromPoint.Call(pt, monitorDefaultToNearest)
	mi := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	x, y := wr.left, wr.top
	if mon != 0 {
		if r, _, _ := getMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			x = mi.rcWork.left + (mi.rcWork.right-mi.rcWork.left-ww)/2
			y = mi.rcWork.top + (mi.rcWork.bottom-mi.rcWork.top-wh)/2
		}
	}

	// Position + pin top-most (keep size), then take foreground focus.
	setWindowPos.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpShowWindow)
	setForegroundWindow.Call(hwnd)

	watchFocusDismiss(hwnd)
}

// watchFocusDismiss exits the process once the foreground window stops being
// ours, after we've actually held the foreground at least once (so we don't
// quit before the window is focused). Mirrors the Linux behaviour. Once the
// user has picked a card (picked), we stop watching: the pick handler hands
// focus back to the editor, which would otherwise look like "clicked away"
// and cancel the selection.
func watchFocusDismiss(hwnd uintptr) {
	seen := false
	for {
		time.Sleep(120 * time.Millisecond)
		if picked.Load() {
			return
		}
		fg, _, _ := getForegroundWindow.Call()
		if fg == hwnd {
			seen = true
			continue
		}
		if seen && !picked.Load() {
			os.Exit(1) // clicked away — cancel, same as Esc
		}
	}
}

// prevForeground is the window that had focus before the picker showed —
// the user's editor. Captured at startup, restored when a card is picked.
var prevForeground uintptr

// notePrevForeground records the foreground window before the picker grabs it.
// Called at the very top of main(), before the Fyne window exists, so it's
// still the editor that spawned us (the parent tray is a background process
// and never took foreground).
func notePrevForeground() { prevForeground, _, _ = getForegroundWindow.Call() }

// restorePrevForeground hands focus back to the editor. Called from the pick
// handler while the picker is still the foreground window, so SetForegroundWindow
// is unrestricted (Windows blocks stealing focus, not giving it away) — unlike
// the parent tray, which can't reliably reclaim it from a background process.
func restorePrevForeground() {
	if prevForeground != 0 {
		setForegroundWindow.Call(prevForeground)
	}
}

// ourWindow returns the first visible, reasonably-sized top-level window owned
// by this process (the Fyne/GLFW window), or 0 if none yet.
func ourWindow() uintptr {
	self := uint32(os.Getpid())
	var found uintptr
	cb := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var pid uint32
		getWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != self {
			return 1 // keep enumerating
		}
		if v, _, _ := isWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var r rect
		getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		if r.right-r.left >= 200 && r.bottom-r.top >= 100 {
			found = hwnd
			return 0 // stop
		}
		return 1
	})
	enumWindows.Call(cb, 0)
	return found
}

type rect struct{ left, top, right, bottom int32 }
type point struct{ x, y int32 }
type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
}

const (
	swpNoSize               = 0x0001
	swpShowWindow           = 0x0040
	monitorDefaultToNearest = 0x0002
)

// hwndTopmost is (HWND)-1 for SetWindowPos's hWndInsertAfter.
var hwndTopmost = ^uintptr(0)

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	enumWindows              = user32.NewProc("EnumWindows")
	getWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible          = user32.NewProc("IsWindowVisible")
	getWindowRect            = user32.NewProc("GetWindowRect")
	setWindowPos             = user32.NewProc("SetWindowPos")
	setForegroundWindow      = user32.NewProc("SetForegroundWindow")
	getForegroundWindow      = user32.NewProc("GetForegroundWindow")
	getCursorPos             = user32.NewProc("GetCursorPos")
	monitorFromPoint         = user32.NewProc("MonitorFromPoint")
	getMonitorInfo           = user32.NewProc("GetMonitorInfoW")
)
