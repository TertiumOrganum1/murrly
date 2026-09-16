package inserter

import "fmt"

// ClipboardBackend is the platform clipboard. Publish makes text the clipboard
// content and returns the func that gives the clipboard back — on X11 that is
// what stops Murrly from owning the desktop's selection for the whole session;
// elsewhere it is a no-op.
type ClipboardBackend interface {
	Publish(text string) (release func(), err error)
	// PublishAndHold publishes without giving the selection back. See the
	// comment on holdAfterPaste for why the insert route wants that.
	PublishAndHold(text string) error
}

// PasterBackend synthesises the paste chord. beforeKey is called at the last
// moment before the keys go down.
type PasterBackend interface {
	Paste(beforeKey func()) error
}

// The selection is not handed back after the chord any more, which is what the
// very first release did too (see a3b15f0: "xclip stays alive as the selection
// owner until a later Set/Restore replaces it").
//
// Handing it back was never free. It is a second ownership change a fifth of a
// second after the paste, and what picks the text up is the desktop's clipboard
// manager — which anything arriving late then has to wait on. Our own owner
// answers in 2 ms; the manager on this machine has twice been logged taking
// over 400 ms. Holding also drops the 200 ms wait that used to sit at the end
// of every insert purely to give a reader time before we killed the owner.
//
// What it costs: while the dictation is what is in the clipboard, a paste of it
// is served by a child process of Murrly. That ends the moment anything else is
// copied — the new owner takes the selection and our xclip exits by itself.

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
	if err := c.CB.PublishAndHold(text); err != nil {
		return fmt.Errorf("clipboard.PublishAndHold: %w", err)
	}
	if err := c.Paster.Paste(func() {}); err != nil {
		return fmt.Errorf("paster.Paste: %w", err)
	}
	return nil
}
