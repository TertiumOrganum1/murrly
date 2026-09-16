package inserter

import (
	"fmt"
	"time"
)

// ClipboardBackend is the platform clipboard. Publish makes text the clipboard
// content and returns the func that gives the clipboard back — on X11 that is
// what stops Murrly from owning the desktop's selection for the whole session;
// elsewhere it is a no-op.
type ClipboardBackend interface {
	Publish(text string) (release func(), err error)
}

// PasterBackend synthesises the paste chord. beforeKey is called at the last
// moment before the keys go down.
type PasterBackend interface {
	Paste(beforeKey func()) error
}

// holdAfterPaste is how long the text stays in the clipboard after the chord
// before we hand the clipboard back.
//
// It is the one number on this route, and it is a margin rather than a
// measurement: applications that read the selection lazily (GTK dialogs,
// terminals) do it within a few tens of milliseconds of the keystroke. What
// used to be here instead — waiting for an observable fetch, adapting the
// pre-chord delay to the machine, holding for three seconds when nothing read
// anything — was built around Chromium/Electron, which never reads the
// selection on Ctrl+V at all. No delay reaches an application that does not
// look, so there is nothing to tune. Ctrl+Shift+F12 exists for those fields.
const holdAfterPaste = 200 * time.Millisecond

// Clipboard delivers text by putting it in the clipboard and pressing the
// paste chord.
//
// The user's own clipboard is not touched here. It is snapshotted into
// Murrly's memory when the recording starts (see app.Config.OnRecordStart),
// which is both earlier — so nothing on the insert path waits for a read of
// somebody else's selection — and cheaper, because it never goes back through
// X. The tray offers it back on request.
type Clipboard struct {
	CB     ClipboardBackend
	Paster PasterBackend
}

func (c *Clipboard) Name() string { return "clipboard" }

func (c *Clipboard) Insert(text string) error {
	if text == "" {
		return nil
	}
	release, err := c.CB.Publish(text)
	if err != nil {
		return fmt.Errorf("clipboard.Publish: %w", err)
	}
	defer release()
	if err := c.Paster.Paste(func() {}); err != nil {
		return fmt.Errorf("paster.Paste: %w", err)
	}
	time.Sleep(holdAfterPaste)
	return nil
}
