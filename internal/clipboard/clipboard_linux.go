//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// xclipReadTimeout caps every blocking xclip -o call. X11 selections are
// served by the owning process, so a hung owner (dead screenshot tool,
// stuck image paste, etc.) used to deadlock our whole insert flow — the
// app goroutine sat in clipboard.Save waiting on xclip waiting on a
// dead owner, and the tray icon was stuck on "transcribing" until the
// user killed the orphan xclip processes by hand. Two seconds is more
// than enough for any sane owner (even a 50 MB image round-trips in
// under a second over the X socket); past that we give up and let the
// dictation still get inserted with whatever clipboard state we have.
const xclipReadTimeout = 2 * time.Second

// xclipOutput runs `xclip args...` with a hard timeout and returns its
// stdout. On timeout it logs once and returns context.DeadlineExceeded
// (which callers translate into "no content, keep going" rather than a
// fatal error).
func xclipOutput(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), xclipReadTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xclip", args...).Output()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		log.Printf("clipboard: xclip %v timed out after %v — likely a hung selection owner; ignoring", args, xclipReadTimeout)
		return nil, context.DeadlineExceeded
	}
	return out, err
}

func (c *Clipboard) Save() (Saved, error) {
	s := Saved{}

	targets, err := readTargets("clipboard")
	if err != nil {
		return s, fmt.Errorf("read clipboard targets: %w", err)
	}
	if len(targets) > 0 {
		s.HasContent = true
		// Pick a non-text MIME (image/png from screenshots, etc.) if
		// the owner advertises one — Save the raw bytes so Restore can
		// re-publish them. Falling through to xclip's default text
		// output would corrupt binary data to UTF-8 mush, which is
		// what was killing user screenshots after a dictation cycle.
		if binTarget := pickBinaryTarget(targets); binTarget != "" {
			data, err := xclipOutput("-selection", "clipboard", "-t", binTarget, "-o")
			if err != nil {
				// Read failed (hung owner, etc.) — don't error out the
				// whole insert; just give up on saving this clipboard so
				// the dictation still gets pasted.
				s.HasContent = false
			} else {
				s.Binary = data
				s.Target = binTarget
			}
		} else {
			out, err := xclipOutput("-selection", "clipboard", "-o")
			if err != nil {
				s.HasContent = false
			} else {
				s.Text = string(out)
			}
		}
	}

	if c.RestorePrimary {
		// X11 primary selection is almost always plain text
		// (highlight-to-copy); skip the binary detour.
		ptargets, err := readTargets("primary")
		if err != nil {
			return s, fmt.Errorf("read primary targets: %w", err)
		}
		if len(ptargets) > 0 {
			out, err := xclipOutput("-selection", "primary", "-o")
			if err == nil {
				s.Primary = string(out)
				s.HasPrimary = true
			}
			// On error: leave HasPrimary false; Restore will skip primary.
		}
	}
	return s, nil
}

func (c *Clipboard) Set(text string) error {
	if err := writeSelection("clipboard", text); err != nil {
		return err
	}
	// xclip claims the selection asynchronously after Start(), so Set used
	// to return before the clipboard actually served `text`. Under load the
	// Ctrl+V that follows could then fire while the PREVIOUS owner was still
	// serving — intermittently pasting the stale clipboard instead of the
	// dictation. Block until the selection really serves `text` (ownership
	// claimed) before returning.
	confirmSelection("clipboard", text, clipboardClaimTimeout)
	return nil
}

// clipboardClaimTimeout bounds how long Set waits for xclip to take
// ownership of the clipboard before giving up and letting the paste proceed
// anyway (best effort — better than blocking the insert forever).
const clipboardClaimTimeout = 1 * time.Second

// confirmSelection polls the selection until it serves `want` (our xclip has
// claimed ownership) or the timeout elapses. Cheap in the common case (the
// claim lands within a few ms → one read); only spins under contention.
func confirmSelection(sel, want string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		out, err := xclipOutput("-selection", sel, "-o")
		if err == nil && string(out) == want {
			return
		}
		if time.Now().After(deadline) {
			log.Printf("clipboard: selection %q not confirmed within %v; pasting anyway", sel, timeout)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (c *Clipboard) Restore(s Saved) error {
	switch {
	case !s.HasContent:
		_ = clearSelection("clipboard")
	case s.Target != "" && len(s.Binary) > 0:
		if err := writeSelectionBinary("clipboard", s.Target, s.Binary); err != nil {
			return fmt.Errorf("restore clipboard %s: %w", s.Target, err)
		}
	default:
		if err := writeSelection("clipboard", s.Text); err != nil {
			return fmt.Errorf("restore clipboard: %w", err)
		}
	}
	if c.RestorePrimary && s.HasPrimary {
		if err := writeSelection("primary", s.Primary); err != nil {
			return fmt.Errorf("restore primary: %w", err)
		}
	}
	return nil
}

// readTargets returns the non-service MIME targets advertised by the
// current selection owner, or an empty slice when the selection is
// empty (xclip returns non-zero exit then — we treat that as "no
// content" rather than an error).
func readTargets(sel string) ([]string, error) {
	out, err := xclipOutput("-selection", sel, "-t", "TARGETS", "-o")
	if err != nil {
		return nil, nil
	}
	return parseTargets(string(out)), nil
}

// pickBinaryTarget returns a target name suitable for round-tripping
// non-text payloads. Image types come first because that's the
// screenshot-then-dictate case the user actually hit; anything other
// than the standard text targets is acceptable as a fallback. Empty
// string means "this clipboard is text — go through the text path".
func pickBinaryTarget(targets []string) string {
	priorities := []string{
		"image/png", "image/jpeg", "image/jpg",
		"image/bmp", "image/gif", "image/webp", "image/tiff",
		"application/pdf",
	}
	for _, p := range priorities {
		for _, t := range targets {
			if t == p {
				return t
			}
		}
	}
	for _, t := range targets {
		if !isTextTarget(t) {
			return t
		}
	}
	return ""
}

func isTextTarget(t string) bool {
	if strings.HasPrefix(t, "text/") {
		return true
	}
	return t == "STRING" || t == "UTF8_STRING" || t == "COMPOUND_TEXT"
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
	go cmd.Wait()
	return nil
}

// writeSelectionBinary mirrors writeSelection for arbitrary MIME types —
// xclip's -t flag advertises the given target so paste requests for
// that MIME are honoured. Same fork-and-detach pattern: xclip stays
// alive holding the selection until a future Set/Restore replaces it.
func writeSelectionBinary(sel, target string, data []byte) error {
	cmd := exec.Command("xclip", "-selection", sel, "-t", target, "-i")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
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
