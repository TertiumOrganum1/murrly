//go:build !linux

package inserter

import "errors"

// Atspi is Linux-only: AT-SPI is the accessibility bus of the free desktop.
// macOS and Windows insert through the clipboard route.
type Atspi struct{}

func (a *Atspi) Name() string { return "atspi" }

func (a *Atspi) Insert(string) error {
	return errors.New("atspi route is not available on this platform")
}
