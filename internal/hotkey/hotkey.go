// Package hotkey grabs a global push-to-talk key on X11.
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
*/
import "C"

import (
	"fmt"
	"strings"
	"time"

	"unsafe"
)

type Event int

const (
	EventDown Event = iota
	EventUp
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
	keysym  C.KeySym
	events  chan Event
	stop    chan struct{}
	pressed bool
	display *C.Display
}

func New(key string) (*Listener, error) {
	sym, ok := x11Keysyms[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return nil, fmt.Errorf("hotkey: unknown key %q (supported: F1..F15)", key)
	}
	return &Listener{
		keysym: sym,
		events: make(chan Event, 8),
		stop:   make(chan struct{}),
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
	l.display = display
	defer func() {
		C.XCloseDisplay(display)
		l.display = nil
	}()
	C.vi_enable_detectable_autorepeat(display)

	root := C.XDefaultRootWindow(display)
	for _, modifiers := range []C.uint{0, C.LockMask, C.Mod2Mask, C.LockMask | C.Mod2Mask} {
		if C.vi_grab_key(display, root, l.keysym, modifiers) == 0 {
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
