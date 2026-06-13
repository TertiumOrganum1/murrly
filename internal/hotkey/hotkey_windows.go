//go:build windows

// hotkey_windows.go implements the Windows backend with a low-level keyboard
// hook (WH_KEYBOARD_LL). RegisterHotKey was rejected because it delivers only
// key-down (no key-up), and push-to-talk needs a reliable release event. The
// LL hook sees every key globally with both press and release, lets us
// dedupe auto-repeat, and lets us SWALLOW the bound key so it doesn't also
// reach the focused app — matching the exclusive XGrabKey behaviour on Linux.
//
// Each Listener installs its own hook on its own OS-locked thread running a
// GetMessage pump (LL hooks fire on the installing thread, which must pump
// messages). Modifier routing mirrors X11's exact-modifier matching: the bare
// listener fires only when Ctrl/Alt are up, the Ctrl listener only when Ctrl
// is down — so plain F12 and Ctrl+F12 never both fire.
package hotkey

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Virtual-key codes for the supported function keys (F1..F15 = 0x70..0x7E).
var vkMap = map[string]uint16{
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74,
	"f6": 0x75, "f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79,
	"f11": 0x7A, "f12": 0x7B, "f13": 0x7C, "f14": 0x7D, "f15": 0x7E,
}

const (
	whKeyboardLL  = 13
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmQuit        = 0x0012
	vkControlCode = 0x11
	vkMenuCode    = 0x12 // Alt
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetWindowsHookEx   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHook  = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx     = user32.NewProc("CallNextHookEx")
	procGetMessage         = user32.NewProc("GetMessageW")
	procGetAsyncKeyState   = user32.NewProc("GetAsyncKeyState")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	procGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

// kbdLLHookStruct mirrors KBDLLHOOKSTRUCT (the LL hook's lParam payload).
type kbdLLHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type Listener struct {
	vk       uint16
	needCtrl bool
	needAlt  bool
	events   chan Event
	pressed  bool

	hook     uintptr
	threadID uint32
	started  chan struct{}
	once     sync.Once
}

func New(key string) (*Listener, error) { return newListener(key, false, false) }

// NewWithCtrl binds Ctrl+<key> (reprocess / picker). Exact-modifier routing
// keeps it from colliding with the bare push-to-talk listener.
func NewWithCtrl(key string) (*Listener, error) { return newListener(key, true, false) }

// NewWithCtrlAlt binds Ctrl+Alt+<key>. Unused on Windows (it backed the
// Linux-only Nemotron picker) but kept for the cross-platform API.
func NewWithCtrlAlt(key string) (*Listener, error) { return newListener(key, true, true) }

func newListener(key string, needCtrl, needAlt bool) (*Listener, error) {
	vk, ok := vkMap[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return nil, fmt.Errorf("hotkey: unknown key %q (supported: F1..F15)", key)
	}
	return &Listener{
		vk:       vk,
		needCtrl: needCtrl,
		needAlt:  needAlt,
		events:   make(chan Event, 8),
		started:  make(chan struct{}),
	}, nil
}

func (l *Listener) Events() <-chan Event { return l.events }

// keyDown reports whether a virtual key is currently physically down.
func keyDown(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&0x8000 != 0
}

// proc is the LowLevelKeyboardProc. It runs on the listener's pump thread.
// lparam is a pointer to a KBDLLHOOKSTRUCT supplied by the OS (not Go-managed
// memory), so the uintptr→Pointer conversion is safe despite go vet's generic
// "possible misuse of unsafe.Pointer" note.
func (l *Listener) proc(nCode uintptr, wparam uintptr, lparam uintptr) uintptr {
	if int32(nCode) >= 0 && l.vk == uint16((*kbdLLHookStruct)(unsafe.Pointer(lparam)).vkCode) {
		switch wparam {
		case wmKeyDown, wmSysKeyDown:
			modsOK := keyDown(vkControlCode) == l.needCtrl && keyDown(vkMenuCode) == l.needAlt
			if modsOK {
				if !l.pressed {
					l.pressed = true
					l.send(EventDown)
				}
				return 1 // swallow — the bound key shouldn't reach the app
			}
		case wmKeyUp, wmSysKeyUp:
			// Release matches regardless of modifier state (the user may
			// lift Ctrl before the key), so the press/release pair always
			// balances and pressed never wedges.
			if l.pressed {
				l.pressed = false
				l.send(EventUp)
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wparam, lparam)
	return r
}

func (l *Listener) send(e Event) {
	select {
	case l.events <- e:
	default:
	}
}

// Start installs the hook and pumps messages until Stop. Runs in its own
// goroutine; pins the OS thread because the LL hook is delivered there.
func (l *Listener) Start() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tid, _, _ := procGetCurrentThreadID.Call()
	l.threadID = uint32(tid)
	close(l.started)

	hmod, _, _ := procGetModuleHandle.Call(0)
	cb := windows.NewCallback(l.proc)
	hook, _, err := procSetWindowsHookEx.Call(whKeyboardLL, cb, hmod, 0)
	if hook == 0 {
		log.Printf("hotkey: SetWindowsHookEx failed: %v", err)
		close(l.events)
		return
	}
	l.hook = hook
	defer procUnhookWindowsHook.Call(l.hook)

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		// GetMessage returns 0 on WM_QUIT and -1 (^uintptr(0)) on error.
		if r == 0 || r == ^uintptr(0) {
			return
		}
	}
}

// Stop unhooks and breaks the message loop by posting WM_QUIT to the pump
// thread. Safe to call once; waits for Start to have recorded its thread id.
func (l *Listener) Stop() {
	l.once.Do(func() {
		<-l.started
		if l.hook != 0 {
			procUnhookWindowsHook.Call(l.hook)
		}
		procPostThreadMessage.Call(uintptr(l.threadID), wmQuit, 0, 0)
	})
}
