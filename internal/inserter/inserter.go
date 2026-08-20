// Package inserter delivers recognized text to the focused field.
//
// Three routes exist, in decreasing order of directness:
//
//	AT-SPI    — hand the text to the accessible object under the caret.
//	            Instant, invisible, and it never touches the clipboard or
//	            the keyboard; limited to apps that expose an editable
//	            accessible field.
//	typing    — synthesise the characters as key events. Works wherever a
//	            keyboard does, at the cost of typing time.
//	clipboard — the legacy route: save the clipboard, put the text in it,
//	            press the paste chord, put the clipboard back.
//
// The clipboard route is last for a reason. X11 selections are served on
// demand by the owning process, but Chromium/Electron applications cache
// the selection when ownership changes and paste from that cache, so the
// instant they actually consume it cannot be observed from outside. Every
// attempt to time the "put it back" step against that unobservable moment
// traded one failure for the other: restore too early and the application
// pastes the OLD clipboard instead of the dictation, restore too late and
// the user's own clipboard stays displaced. The direct routes have no such
// window because nothing is ever borrowed.
package inserter

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Inserter delivers text to wherever the caret is.
type Inserter interface {
	// Insert returns nil only when the text actually reached the field.
	// Any error means "this route did not deliver" and lets a chain move
	// on to the next one.
	Insert(text string) error
	// Name identifies the route in logs.
	Name() string
}

// settleDelay waits for the push-to-talk key (F12 / Break) to finish
// releasing before anything is inserted. Without it the modifier race —
// the physical key still going up while we synthesise input — corrupts the
// first characters, and an application still processing the keyup can move
// the caret out from under an accessible-object write.
const settleDelay = 300 * time.Millisecond

// Chain tries its routes in order and stops at the first one that delivers.
type Chain struct {
	routes []Inserter
	// Settle is waited out ONCE before the first route, not per route:
	// the key release happens once per dictation, and sleeping again for
	// each fallback is latency the user watches accumulate.
	Settle time.Duration
}

func NewChain(routes ...Inserter) *Chain { return &Chain{routes: routes} }

func (c *Chain) Name() string {
	names := make([]string, 0, len(c.routes))
	for _, r := range c.routes {
		names = append(names, r.Name())
	}
	return strings.Join(names, "→")
}

// Insert walks the routes until one succeeds. Failures are collected rather
// than discarded: when nothing delivered, the returned error names every
// route and what it complained about, which is the only diagnostic the user
// gets for a dictation that silently went nowhere.
func (c *Chain) Insert(text string) error {
	if len(c.routes) == 0 {
		return errors.New("insert: no route configured")
	}
	if c.Settle > 0 {
		time.Sleep(c.Settle)
	}
	var failures []string
	for _, r := range c.routes {
		err := r.Insert(text)
		if err == nil {
			if len(failures) > 0 {
				log.Printf("insert: delivered via %s after %s", r.Name(), strings.Join(failures, "; "))
			}
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", r.Name(), err))
	}
	return fmt.Errorf("insert: every route failed (%s)", strings.Join(failures, "; "))
}

// Insert modes, mirroring the values accepted by the config's insert_mode.
const (
	ModeHybrid    = "hybrid"
	ModeAtspi     = "atspi"
	ModeType      = "type"
	ModeClipboard = "clipboard"
)

// ForMode builds the chain of routes for a configured mode. A clipboard
// backend of nil (no clipboard wired on this build) simply drops that
// route from the chain rather than leaving a hole to trip over later.
//
// An unrecognised mode gets the hybrid chain: the config layer normalises
// values, so reaching here with something else means a caller bypassed it,
// and inserting the dictation by the most compatible route beats dropping
// it on the floor.
func ForMode(mode string, typeDelayMs int, cb ClipboardBackend, p PasterBackend, pasteDelay time.Duration) *Chain {
	clip := func() []Inserter {
		if cb == nil || p == nil {
			return nil
		}
		return []Inserter{&Clipboard{CB: cb, Paster: p, PasteDelay: pasteDelay}}
	}
	var c *Chain
	switch mode {
	case ModeAtspi:
		c = NewChain(&Atspi{})
	case ModeType:
		c = NewChain(&Typing{DelayMs: typeDelayMs})
	case ModeClipboard:
		c = NewChain(clip()...)
	default:
		routes := []Inserter{&Atspi{}, &Typing{DelayMs: typeDelayMs}}
		c = NewChain(append(routes, clip()...)...)
	}
	c.Settle = settleDelay
	return c
}
