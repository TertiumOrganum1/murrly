// Package clipboard saves and restores the X11 clipboard via the xclip subprocess.
// MVP: plain text only. Multi-MIME restoration is iteration 2 (see spec section 6).
package clipboard

import (
	"fmt"
	"os/exec"
	"strings"
)

type Saved struct {
	Text       string
	HasContent bool
	Primary    string
	HasPrimary bool
}

type Clipboard struct {
	RestorePrimary bool
}

func New() *Clipboard {
	return &Clipboard{RestorePrimary: true}
}

func (c *Clipboard) Save() (Saved, error) {
	s := Saved{}

	text, ok, err := readSelection("clipboard")
	if err != nil {
		return s, fmt.Errorf("read clipboard: %w", err)
	}
	s.Text = text
	s.HasContent = ok

	if c.RestorePrimary {
		ptext, pok, err := readSelection("primary")
		if err != nil {
			return s, fmt.Errorf("read primary: %w", err)
		}
		s.Primary = ptext
		s.HasPrimary = pok
	}
	return s, nil
}

func (c *Clipboard) Set(text string) error {
	return writeSelection("clipboard", text)
}

func (c *Clipboard) Restore(s Saved) error {
	if s.HasContent {
		if err := writeSelection("clipboard", s.Text); err != nil {
			return fmt.Errorf("restore clipboard: %w", err)
		}
	} else {
		_ = clearSelection("clipboard")
	}
	if c.RestorePrimary && s.HasPrimary {
		if err := writeSelection("primary", s.Primary); err != nil {
			return fmt.Errorf("restore primary: %w", err)
		}
	}
	return nil
}

func readSelection(sel string) (string, bool, error) {
	targets, err := exec.Command("xclip", "-selection", sel, "-t", "TARGETS", "-o").Output()
	if err != nil {
		// xclip returns non-zero when selection is empty.
		return "", false, nil
	}
	parsed := parseTargets(string(targets))
	if len(parsed) == 0 {
		return "", false, nil
	}
	out, err := exec.Command("xclip", "-selection", sel, "-o").Output()
	if err != nil {
		return "", true, err
	}
	return string(out), true, nil
}

// writeSelection writes `text` into the X selection and detaches xclip into
// the background. xclip stays alive as the selection owner (that is how X11
// selections work — the owning process serves paste requests) until a later
// Set/Restore replaces it. We must Start() rather than Run(): xclip does not
// fork off, so Run() would block for the whole lifetime of the ownership.
func writeSelection(sel, text string) error {
	cmd := exec.Command("xclip", "-selection", sel, "-i")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap once xclip loses ownership and exits
	return nil
}

func clearSelection(sel string) error {
	return writeSelection(sel, "")
}

// parseTargets filters service entries out of an xclip TARGETS list.
func parseTargets(raw string) []string {
	skip := map[string]bool{
		"TARGETS":      true,
		"TIMESTAMP":    true,
		"MULTIPLE":     true,
		"SAVE_TARGETS": true,
	}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || skip[line] {
			continue
		}
		out = append(out, line)
	}
	return out
}
