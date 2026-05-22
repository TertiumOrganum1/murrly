//go:build darwin

package clipboard

import (
	"fmt"
	"os/exec"
	"strings"
)

// Save reads the macOS pasteboard via pbpaste.
// macOS has no separate "primary" selection — Primary/HasPrimary stay zero.
func (c *Clipboard) Save() (Saved, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		// pbpaste returns 0 even for empty clipboard; non-zero means something
		// went wrong. Treat as empty rather than failing the round-trip.
		return Saved{}, nil
	}
	text := string(out)
	return Saved{
		Text:       text,
		HasContent: len(text) > 0,
	}, nil
}

// Set writes text to the macOS pasteboard via pbcopy.
func (c *Clipboard) Set(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pbcopy: %w", err)
	}
	return nil
}

// Restore writes the previously saved text back to the pasteboard.
// RestorePrimary is a no-op on macOS (no primary selection concept).
func (c *Clipboard) Restore(s Saved) error {
	if s.HasContent {
		return c.Set(s.Text)
	}
	// Clear the pasteboard by writing an empty string.
	return c.Set("")
}
