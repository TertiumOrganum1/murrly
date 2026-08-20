//go:build linux

package uicontext

import (
	"context"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"
)

const (
	ifaceEditableText = "org.a11y.atspi.EditableText"

	// insertTimeout bounds the whole insertion (focus resolution plus the
	// write round-trips). Larger than captureTimeout because writing costs
	// more calls than reading; still short enough that a wedged
	// accessibility bus falls through to the next route quickly instead of
	// leaving the user staring at nothing.
	insertTimeout = 1200 * time.Millisecond
)

// InsertAtCaret writes text straight into the focused accessible field via
// AT-SPI, replacing the selection if there is one.
//
// This is the route that touches nothing else: no clipboard to borrow and
// give back, no synthetic keystrokes for the application to interpret. It
// only works where the toolkit exposes an editable accessible object —
// GTK and Qt do, Chromium/Electron do when accessibility is on, terminals
// generally do not. The error return is what tells a caller to try
// another route, so it must be honest: a refused or unverifiable write
// reports failure rather than silently swallowing the dictation.
func InsertAtCaret(text string) error {
	if text == "" {
		return nil
	}
	ensureFocusTracker()
	ctx, cancel := context.WithTimeout(context.Background(), insertTimeout)
	defer cancel()

	pid, rect, err := activeWindow(ctx)
	if err != nil {
		return fmt.Errorf("no active window: %w", err)
	}
	conn, err := getA11yConn()
	if err != nil {
		return fmt.Errorf("no a11y bus: %w", err)
	}
	c := &atspiClient{ctx: ctx, conn: conn}
	target, ok := c.insertTarget(pid, rect)
	if !ok {
		return fmt.Errorf("no editable accessible field in the focused window (pid=%d)", pid)
	}
	return c.insertInto(target, text)
}

// insertTarget resolves WHERE to write, mirroring capture's focus logic but
// keeping only editable candidates — reading tolerates a read-only element,
// writing does not.
func (c *atspiClient) insertTarget(pid uint32, rect winRect) (ref, bool) {
	apps, err := c.appsForPID(pid)
	if err != nil || len(apps) == 0 {
		return ref{}, false
	}
	var matches []ref
	for _, app := range apps {
		m, err := c.focusedIn(app)
		if err != nil {
			continue
		}
		matches = append(matches, m...)
	}
	if len(matches) == 0 {
		return ref{}, false
	}

	// Best signal: the element that LAST gained focus is the one the user's
	// keystrokes reach, so it is where the dictation belongs — provided it
	// lives in the active window (the same guard capture needs against a
	// stale focus in another window of the same process).
	if f, ok := c.trackedFocus(); ok {
		for _, app := range apps {
			if app.Name != f.Name {
				continue
			}
			if c.inActiveRect(f, rect) && c.isEditable(f) {
				return f, true
			}
			break
		}
	}

	// Fallback: prefer a field inside the active window, then the smallest
	// one. VS Code marks both the chat input and the open document's mirror
	// focused; the user dictates into the small input.
	type cand struct {
		ref    ref
		inRect bool
		chars  int
	}
	cands := make([]cand, 0, len(matches))
	for _, m := range matches {
		if !c.isEditable(m) {
			continue
		}
		n, err := c.charCount(m)
		if err != nil {
			n = 1 << 30
		}
		cands = append(cands, cand{ref: m, inRect: c.inActiveRect(m, rect), chars: n})
	}
	if len(cands) == 0 {
		return ref{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].inRect != cands[j].inRect {
			return cands[i].inRect
		}
		return cands[i].chars < cands[j].chars
	})
	return cands[0].ref, true
}

// inActiveRect reports whether the element's centre sits inside the active
// window. Only h>0 is required, not w>0: VS Code exposes its live editor
// line as a zero-WIDTH sliver, and demanding width drops the real field.
func (c *atspiClient) inActiveRect(r ref, rect winRect) bool {
	if !rect.known() {
		return true // unknown geometry — don't reject on it
	}
	x, y, w, h, ok := c.extents(r)
	if !ok || h <= 0 {
		return false
	}
	return rect.contains(x+w/2, y+h/2)
}

// insertInto writes text at the caret of r, replacing any selection first
// (a paste would have replaced it, and the transform upstream already reads
// the field as if it had).
//
// The write is verified by character count rather than trusted: some
// toolkits answer InsertText with true and do nothing, and an unnoticed
// no-op would drop the user's dictation on the floor with no fallback.
func (c *atspiClient) insertInto(r ref, text string) error {
	before, beforeErr := c.charCount(r)

	at, err := c.caretOffset(r)
	if err != nil {
		return fmt.Errorf("read caret: %w", err)
	}
	if start, end, ok := c.selection(r); ok {
		var deleted bool
		if err := c.call(r, ifaceEditableText+".DeleteText", int32(start), int32(end)).Store(&deleted); err != nil {
			return fmt.Errorf("replace selection: %w", err)
		}
		if !deleted {
			return fmt.Errorf("field refused to replace the selection")
		}
		at = start
		if beforeErr == nil {
			before -= end - start
		}
	}

	runes := utf8.RuneCountInString(text)
	var ok bool
	if err := c.call(r, ifaceEditableText+".InsertText", int32(at), text, int32(runes)).Store(&ok); err != nil {
		return fmt.Errorf("insert text: %w", err)
	}
	if !ok {
		return fmt.Errorf("field refused the insert")
	}
	if beforeErr == nil {
		if after, err := c.charCount(r); err == nil && after <= before {
			return fmt.Errorf("field reported success but its content did not grow (%d → %d)", before, after)
		}
	}
	// Leave the caret after the inserted text, where the user expects to
	// keep typing. Best effort: a field that ignores this is still correct,
	// just positioned as the toolkit sees fit.
	_ = c.call(r, ifaceText+".SetCaretOffset", int32(at+runes)).Store(new(bool))
	return nil
}
