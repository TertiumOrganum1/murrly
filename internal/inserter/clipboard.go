package inserter

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// ClipboardBackend is the platform clipboard. Save returns an opaque
// snapshot handed back to Restore; the route never introspects it.
type ClipboardBackend interface {
	Save() (any, error)
	Set(string) error
	Restore(any) error
}

// FetchReporter is implemented by backends that can say when their
// publication was read. Used only to report timings: choosing how long to
// hold the clipboard is a judgement about real applications, and this is
// the measurement it should be based on.
type FetchReporter interface {
	FetchTimes() []time.Time
}

// ClipboardConfirmer is implemented by backends that can re-check their own
// ownership of the clipboard. Used right before the paste chord: if
// something claimed the selection back in the meantime, pressing Ctrl+V
// would paste that instead of the dictation.
type ClipboardConfirmer interface {
	// ServesText reports whether the clipboard currently serves exactly
	// this text, i.e. our own publication is still the live one.
	ServesText(text string) bool
}

// PasterBackend synthesises the paste chord. beforeKey is called at the
// last moment before the keys go down.
type PasterBackend interface {
	Paste(beforeKey func()) error
}

// prePasteDelay sits between publishing the text and pressing the paste
// chord.
//
// It exists because Chromium/Electron applications do not read the
// selection when you paste — they keep a cached copy and refresh it when
// the X server tells them ownership changed. Press the chord before that
// notification has been processed and the application pastes its stale
// cache: the user's OLD clipboard instead of the dictation. Nothing
// observable from outside says when that refresh happened, so the only
// honest answer is to give it room.
const prePasteDelay = 250 * time.Millisecond

// postPasteDelay is how long the text stays in the clipboard after the
// chord, before the user's own content goes back.
//
// It is insurance, not the load-bearing part. Measured against a live
// Electron application, every read of the selection happened BEFORE the
// chord — the last one 53 ms before it — and none after: the application
// pastes from the cache above and never asks again. An earlier version
// waited for a read AFTER the chord before restoring, which in such an
// application is an event that never arrives; it timed out on every single
// insert. This window is here for the applications that DO read lazily
// (GTK dialogs, terminals), and is kept short because the user's own
// clipboard is displaced for exactly this long — long enough and a Ctrl+V
// right after dictating hands them the dictation back.
const postPasteDelay = 300 * time.Millisecond

// Clipboard is the legacy route: borrow the clipboard, paste, give it back.
// Kept as the last fallback for fields that expose neither an accessible
// object nor a working keyboard path.
type Clipboard struct {
	CB         ClipboardBackend
	Paster     PasterBackend
	PasteDelay time.Duration
}

func (c *Clipboard) Name() string { return "clipboard" }

func (c *Clipboard) Insert(text string) error {
	if text == "" {
		return nil
	}
	saved, err := c.CB.Save()
	if err != nil {
		return fmt.Errorf("clipboard.Save: %w", err)
	}
	// From here on the user's clipboard is displaced, so every exit path
	// below has to put it back.
	restore := func() {
		if err := c.CB.Restore(saved); err != nil {
			log.Printf("clipboard.Restore: %v", err)
		}
	}
	if err := c.CB.Set(text); err != nil {
		restore()
		return fmt.Errorf("clipboard.Set: %w", err)
	}
	// Let the target application notice the new clipboard before the chord.
	time.Sleep(prePasteDelay)
	// And check we are still the one serving it: a clipboard manager or
	// another application can claim the selection in that window, and
	// pasting then would deliver their content, not the dictation.
	if conf, ok := c.CB.(ClipboardConfirmer); ok && !conf.ServesText(text) {
		log.Printf("insert: lost the clipboard before pasting; republishing")
		if err := c.CB.Set(text); err != nil {
			restore()
			return fmt.Errorf("clipboard.Set (republish): %w", err)
		}
		time.Sleep(prePasteDelay)
	}
	if err := c.Paster.Paste(func() {}); err != nil {
		restore()
		return fmt.Errorf("paster.Paste: %w", err)
	}
	chord := time.Now()
	// Hold the dictation in the clipboard long enough for the application
	// to have read it, then give the user their content back. PasteDelay
	// can extend this window but not shorten it below what the caching
	// applications need.
	hold := postPasteDelay
	if c.PasteDelay > hold {
		hold = c.PasteDelay
	}
	time.Sleep(hold)
	if rep, ok := c.CB.(FetchReporter); ok {
		var offsets []string
		for _, t := range rep.FetchTimes() {
			offsets = append(offsets, t.Sub(chord).Round(time.Millisecond).String())
		}
		log.Printf("clipboard: held %v; reads relative to the chord: %s", hold, strings.Join(offsets, " "))
	}
	restore()
	return nil
}
