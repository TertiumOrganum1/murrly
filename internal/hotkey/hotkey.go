// Package hotkey listens for a global push-to-talk key on X11 via gohook.
package hotkey

import (
	"fmt"
	"strings"

	hook "github.com/robotn/gohook"
)

type Event int

const (
	EventDown Event = iota
	EventUp
)

// X11 keysyms — what gohook reports as ev.Rawcode on Linux/X11.
// We use these instead of hook.Keycode because that table contains
// Windows-style VK codes which do not match what gohook delivers on X11.
var x11Keysyms = map[string]uint16{
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
	keysym  uint16
	events  chan Event
	stop    chan struct{}
	pressed bool
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
	ch := hook.Start()
	defer hook.End()

	for {
		select {
		case <-l.stop:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Rawcode != l.keysym {
				continue
			}
			switch ev.Kind {
			case hook.KeyDown, hook.KeyHold:
				if !l.pressed {
					l.pressed = true
					l.events <- EventDown
				}
			case hook.KeyUp:
				if l.pressed {
					l.pressed = false
					l.events <- EventUp
				}
			}
		}
	}
}

func (l *Listener) Stop() {
	close(l.stop)
	hook.End()
}
