//go:build linux

package inserter

import "github.com/tertiumorganum1/murrly/internal/uicontext"

// Atspi writes the text straight into the focused accessible field. It is
// the first route tried because it is the only one with no side channel at
// all: the clipboard is untouched and no key events are synthesised, so
// nothing can race it and nothing of the user's is borrowed.
type Atspi struct{}

func (a *Atspi) Name() string { return "atspi" }

func (a *Atspi) Insert(text string) error {
	if text == "" {
		return nil
	}
	return uicontext.InsertAtCaret(text)
}
