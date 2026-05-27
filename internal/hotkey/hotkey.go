//go:build linux

// hotkey.go implements the Linux X11 backend.
package hotkey

/*
#cgo linux LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <X11/XKBlib.h>
#include <X11/keysym.h>

static int vi_grab_key(Display* display, Window root, KeySym keysym, unsigned int modifiers) {
	KeyCode keycode = XKeysymToKeycode(display, keysym);
	if (keycode == 0) {
		return 0;
	}
	XGrabKey(display, keycode, modifiers, root, False, GrabModeAsync, GrabModeAsync);
	return 1;
}

static KeySym vi_keycode_to_keysym(Display* display, unsigned int keycode) {
	return XkbKeycodeToKeysym(display, (KeyCode)keycode, 0, 0);
}

static void vi_enable_detectable_autorepeat(Display* display) {
	Bool supported = False;
	XkbSetDetectableAutoRepeat(display, True, &supported);
}

// vi_x_error_handler swallows X protocol errors instead of letting
// Xlib's default handler call exit(). The one we actually expect is
// BadAccess from XGrabKey when a key+modifier combo is already grabbed
// by the window manager or another client (e.g. Alt+F12 taken by the
// DE). A failed grab just means that binding won't fire; the process —
// and the other, successful grabs — must survive.
static int vi_x_error_handler(Display* d, XErrorEvent* e) {
	return 0;
}

static void vi_install_error_handler(void) {
	XSetErrorHandler(vi_x_error_handler);
}
*/
import "C"

import (
	"fmt"
	"strings"
	"time"

	"unsafe"
)

// X11 keysyms for function keys.
var x11Keysyms = map[string]C.KeySym{
	"f1":  0xFFBE,
	"f2":  0xFFBF,
	"f3":  0xFFC0,
	"f4":  0xFFC1,
	"f5":  0xFFC2,
	"f6":  0xFFC3,
	"f7":  0xFFC4,
	"f8":  0xFFC5,
	"f9":  0xFFC6,
	"f10": 0xFFC7,
	"f11": 0xFFC8,
	"f12": 0xFFC9,
	"f13": 0xFFCA,
	"f14": 0xFFCB,
	"f15": 0xFFCC,
}

type Listener struct {
	keysym   C.KeySym
	modifier C.uint // 0 = bare key, or ControlMask etc. for combinations
	events   chan Event
	stop     chan struct{}
	pressed  bool
	display  *C.Display
}

func New(key string) (*Listener, error) {
	return newListener(key, 0)
}

// NewWithCtrl creates a Listener bound to Ctrl+<key>. Used as the
// reprocess hotkey alongside the bare push-to-talk binding so the
// two don't fight each other: X11 routes events by exact modifier
// state, so Ctrl+F12 lands on this listener while plain F12 stays
// on the New() listener.
func NewWithCtrl(key string) (*Listener, error) {
	return newListener(key, C.ControlMask)
}

// NewWithCtrlAlt creates a Listener bound to Ctrl+Alt+<key>
// (Control+Mod1). Used as the multi-inference picker hotkey — a
// three-key combo that's far less likely to collide with a window-
// manager shortcut than plain Alt+<key> (which Cinnamon already grabs).
func NewWithCtrlAlt(key string) (*Listener, error) {
	return newListener(key, C.ControlMask|C.Mod1Mask)
}

func newListener(key string, modifier C.uint) (*Listener, error) {
	sym, ok := x11Keysyms[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return nil, fmt.Errorf("hotkey: unknown key %q (supported: F1..F15)", key)
	}
	return &Listener{
		keysym:   sym,
		modifier: modifier,
		events:   make(chan Event, 8),
		stop:     make(chan struct{}),
	}, nil
}

func (l *Listener) Events() <-chan Event { return l.events }

// Start begins listening. Blocks until Stop is called from another goroutine;
// intended to be invoked from its own goroutine.
func (l *Listener) Start() {
	display := C.XOpenDisplay(nil)
	if display == nil {
		return
	}
	// Make grab failures (BadAccess on an already-taken combo) non-fatal
	// instead of letting Xlib's default handler exit the process.
	C.vi_install_error_handler()
	l.display = display
	defer func() {
		C.XCloseDisplay(display)
		l.display = nil
	}()
	C.vi_enable_detectable_autorepeat(display)

	root := C.XDefaultRootWindow(display)
	// Grab the key for every lock-state combination on top of the
	// caller-supplied modifier so CapsLock/NumLock don't gate the
	// hotkey. l.modifier == 0 covers the bare-key case (push-to-talk);
	// non-zero values (e.g. ControlMask) cover modifier+key combos
	// like Ctrl+F12 used for reprocess.
	for _, lockState := range []C.uint{0, C.LockMask, C.Mod2Mask, C.LockMask | C.Mod2Mask} {
		if C.vi_grab_key(display, root, l.keysym, l.modifier|lockState) == 0 {
			return
		}
	}
	C.XSelectInput(display, root, C.KeyPressMask|C.KeyReleaseMask)
	C.XFlush(display)

	for {
		select {
		case <-l.stop:
			return
		default:
		}

		if C.XPending(display) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		var event C.XEvent
		C.XNextEvent(display, &event)
		eventType := xEventType(&event)
		if eventType != C.KeyPress && eventType != C.KeyRelease {
			continue
		}
		keyEvent := (*C.XKeyEvent)(unsafe.Pointer(&event))
		if C.vi_keycode_to_keysym(display, C.uint(keyEvent.keycode)) != l.keysym {
			continue
		}
		switch eventType {
		case C.KeyPress:
			if !l.pressed {
				l.pressed = true
				l.events <- EventDown
			}
		case C.KeyRelease:
			if l.pressed {
				l.pressed = false
				l.events <- EventUp
			}
		}
	}
}

func (l *Listener) Stop() {
	close(l.stop)
	if l.display != nil {
		C.XFlush(l.display)
	}
}

func xEventType(event *C.XEvent) C.int {
	return *(*C.int)(unsafe.Pointer(event))
}
