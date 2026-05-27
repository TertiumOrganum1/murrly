//go:build darwin

// hotkey_darwin.go implements the macOS backend via Carbon RegisterEventHotKey
// (wrapped by golang.design/x/hotkey). Carbon natively delivers press and
// release events; no Accessibility permission is required to register the
// hotkey itself. (Accessibility is still required for paster.Paste which
// sends Cmd+V via osascript.)
package hotkey

import (
	"fmt"
	"log"
	"strings"

	gohotkey "golang.design/x/hotkey"
)

// keyMap is the supported set of keys for push-to-talk on macOS. We keep this
// short and aligned with the Linux implementation (F1..F15).
var keyMap = map[string]gohotkey.Key{
	"f1":  gohotkey.KeyF1,
	"f2":  gohotkey.KeyF2,
	"f3":  gohotkey.KeyF3,
	"f4":  gohotkey.KeyF4,
	"f5":  gohotkey.KeyF5,
	"f6":  gohotkey.KeyF6,
	"f7":  gohotkey.KeyF7,
	"f8":  gohotkey.KeyF8,
	"f9":  gohotkey.KeyF9,
	"f10": gohotkey.KeyF10,
	"f11": gohotkey.KeyF11,
	"f12": gohotkey.KeyF12,
	"f13": gohotkey.KeyF13,
	"f14": gohotkey.KeyF14,
	"f15": gohotkey.KeyF15,
}

type Listener struct {
	hk     *gohotkey.Hotkey
	events chan Event
	stop   chan struct{}
}

func New(key string) (*Listener, error) {
	k, ok := keyMap[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return nil, fmt.Errorf("hotkey: unknown key %q (supported: F1..F15)", key)
	}
	return &Listener{
		hk:     gohotkey.New(nil, k),
		events: make(chan Event, 8),
		stop:   make(chan struct{}),
	}, nil
}

// NewWithCtrl creates a Listener bound to Ctrl+<key> on macOS via
// golang.design/x/hotkey's modifier list. Used for the reprocess
// binding so it doesn't collide with the bare push-to-talk hotkey.
func NewWithCtrl(key string) (*Listener, error) {
	return newModified(key, gohotkey.ModCtrl)
}

// NewWithCtrlAlt creates a Listener bound to Ctrl+Alt(Option)+<key> on
// macOS. Used for the multi-inference picker hotkey.
func NewWithCtrlAlt(key string) (*Listener, error) {
	return newModified(key, gohotkey.ModCtrl|gohotkey.ModOption)
}

func newModified(key string, mod gohotkey.Modifier) (*Listener, error) {
	k, ok := keyMap[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return nil, fmt.Errorf("hotkey: unknown key %q (supported: F1..F15)", key)
	}
	return &Listener{
		hk:     gohotkey.New([]gohotkey.Modifier{mod}, k),
		events: make(chan Event, 8),
		stop:   make(chan struct{}),
	}, nil
}

func (l *Listener) Events() <-chan Event { return l.events }

// Start registers the hotkey and pipes press/release events to Events().
// Blocks until Stop is called; intended to be run in its own goroutine.
//
// If registration fails (e.g. another app already owns the key), the error is
// logged and the events channel is closed so consumers ranging over Events()
// unblock cleanly. Stop() in that case is a no-op.
func (l *Listener) Start() {
	if err := l.hk.Register(); err != nil {
		log.Printf("hotkey: register failed: %v", err)
		close(l.events)
		return
	}
	for {
		select {
		case <-l.stop:
			_ = l.hk.Unregister()
			close(l.events)
			return
		case <-l.hk.Keydown():
			l.events <- EventDown
		case <-l.hk.Keyup():
			l.events <- EventUp
		}
	}
}

func (l *Listener) Stop() {
	close(l.stop)
}
