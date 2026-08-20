//go:build !linux

package inserter

import "errors"

// Typing is Linux-only for now: the macOS and Windows builds insert
// through the clipboard route, which is what they have always used.
type Typing struct{ DelayMs int }

func (t *Typing) Name() string { return "typing" }

func (t *Typing) Insert(string) error {
	return errors.New("typing route is not implemented on this platform")
}
