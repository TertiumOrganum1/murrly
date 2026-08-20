package inserter

import (
	"fmt"
	"log"
	"time"
)

// ClipboardBackend is the platform clipboard. Save returns an opaque
// snapshot handed back to Restore; the route never introspects it.
type ClipboardBackend interface {
	Save() (any, error)
	Set(string) error
	Restore(any) error
}

// PasteWaiter is implemented by clipboard backends that can observe the
// target application fetching what was published (the X11 backend watches
// its selection owner). It narrows the window in which the user's own
// clipboard is displaced, but it cannot close it: applications that cache
// the selection on ownership change paste from that cache, and no
// observation from outside can see that happen. Which is exactly why this
// route sits last in the default chain.
type PasteWaiter interface {
	ArmPasteWait()
	WaitPasted(timeout time.Duration) bool
}

// PasterBackend synthesises the paste chord. beforeKey is called at the
// last moment before the keys go down.
type PasterBackend interface {
	Paste(beforeKey func()) error
}

// pasteWaitTimeout bounds the wait for the paste to be observed. Every
// millisecond here is time the user's clipboard is still displaced by the
// dictation, so a Ctrl+V in that window hands them the dictation back
// instead of what they copied.
const pasteWaitTimeout = 1200 * time.Millisecond

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
	w, canWait := c.CB.(PasteWaiter)
	beforeKey := func() {
		if canWait {
			w.ArmPasteWait()
		}
	}
	if err := c.Paster.Paste(beforeKey); err != nil {
		restore()
		return fmt.Errorf("paster.Paste: %w", err)
	}
	if canWait && !w.WaitPasted(pasteWaitTimeout) {
		log.Printf("insert: paste not observed within %v; restoring clipboard anyway", pasteWaitTimeout)
	}
	time.Sleep(c.PasteDelay)
	restore()
	return nil
}
