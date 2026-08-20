//go:build linux

package inserter

import (
	"fmt"
	"os/exec"
	"strconv"
)

// minTypeDelay is the floor for the per-keystroke pause. Zero (or worse, a
// negative from a hand-edited config) is not "as fast as possible" — it is
// faster than several toolkits process XTEST events, and they silently drop
// characters.
const minTypeDelay = 1

// Typing synthesises the text as key events via xdotool. It never touches
// the clipboard, so the user's copied content is untouched and there is no
// paste to race — the trade is that a long phrase takes DelayMs per
// character to arrive.
type Typing struct {
	// DelayMs is the per-keystroke pause; values below minTypeDelay are
	// raised to it.
	DelayMs int
}

func (t *Typing) Name() string { return "typing" }

func (t *Typing) Insert(text string) error {
	if text == "" {
		return nil
	}
	out, err := exec.Command("xdotool", typeArgs(t.DelayMs, text)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("xdotool type: %w (%s)", err, out)
	}
	return nil
}

// typeArgs builds the xdotool argument list. The "--" terminator matters:
// dictations legitimately start with a dash, and without it xdotool would
// read the text as its own options.
func typeArgs(delayMs int, text string) []string {
	if delayMs < minTypeDelay {
		delayMs = minTypeDelay
	}
	return []string{"type", "--clearmodifiers", "--delay", strconv.Itoa(delayMs), "--", text}
}
