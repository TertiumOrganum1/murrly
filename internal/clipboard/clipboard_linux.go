//go:build linux

package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// pasteTracker holds the verbose xclip owner spawned by the last Set so
// WaitPasted can observe when the pasted text is actually fetched by the
// target application. The darwin/windows clipboards have no observable
// owner process; their pasteTracker is an empty struct.
type pasteTracker struct {
	mu sync.Mutex
	// owner is the verbose xclip publishing the text from the last Set.
	owner *xclipOwner
	// armedAt is owner.served as of ArmPasteWait — the fetch count at the
	// moment just before the paste keystroke. WaitPasted waits for the
	// counter to move past it.
	armedAt int64
}

// xclipOwner tracks one `xclip -i -verbose` selection owner. served counts
// completed content transfers (xclip does not count TARGETS requests).
// WaitPasted snapshots it on entry and waits for it to grow, so only
// fetches that happen after the call — i.e. the target application's
// paste — are detected; earlier fetches (Set's confirmation read, desktop
// snoopers that burst-read on ownership change) are absorbed.
type xclipOwner struct {
	served atomic.Int64
	// times records when each content transfer completed, so the insert
	// path can report how long after the paste chord the application
	// actually read the text — the only honest basis for choosing how long
	// to hold the user's clipboard hostage.
	mu    sync.Mutex
	times []time.Time
}

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
	owner, err := writeSelectionTracked("clipboard", text)
	if err != nil {
		return err
	}
	// xclip claims the selection asynchronously after Start(), so Set used
	// to return before the clipboard actually served `text`. Under load the
	// Ctrl+V that follows could then fire while the PREVIOUS owner was still
	// serving — intermittently pasting the stale clipboard instead of the
	// dictation. Block until the selection really serves `text` (ownership
	// claimed) before returning.
	confirmSelection("clipboard", text, clipboardClaimTimeout)
	c.mu.Lock()
	c.owner = owner
	c.mu.Unlock()
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

var requestNumberRe = regexp.MustCompile(`Waiting for selection request number (\d+)`)

// parseRequestNumber extracts N from xclip -verbose's "Waiting for
// selection request number N" stderr lines. ok=false for any other line.
func parseRequestNumber(line string) (int, bool) {
	m := requestNumberRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ArmPasteWait records the current fetch count as the baseline for the
// paste that is about to happen. The paster calls it immediately before
// pressing Ctrl+V — everything fetched before that point (Set's own
// confirmation read, desktop clipboard snoopers that burst-read on every
// ownership change) belongs to the past and must not be mistaken for the
// paste. Arming after the keystroke instead is a race the fast apps win:
// they fetch while Paste is still returning, the fetch lands inside the
// baseline, and the wait below then times out on every single insert.
func (c *Clipboard) ArmPasteWait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner == nil {
		return
	}
	c.armedAt = c.owner.served.Load()
}

// WaitPasted blocks until the text published by the last Set is fetched
// at least once after ArmPasteWait — i.e. the target application actually
// pasted it — or the timeout elapses. Returns true when the fetch was
// observed.
func (c *Clipboard) WaitPasted(timeout time.Duration) bool {
	c.mu.Lock()
	owner, base := c.owner, c.armedAt
	c.mu.Unlock()
	if owner == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		if owner.served.Load() > base {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// writeSelectionTracked writes `text` into the X selection like
// writeSelection, but runs xclip with -verbose (which also keeps it in
// the foreground) and scans its stderr for "Waiting for selection request
// number N" lines — xclip prints "number N+1" right after completing
// content transfer N, which is how WaitPasted knows the target application
// really fetched the dictation. The process still lives until a later
// Set/Restore replaces it as selection owner (SelectionClear → xclip
// exits → the scanner goroutine drains and reaps it).
func writeSelectionTracked(sel, text string) (*xclipOwner, error) {
	cmd := exec.Command("xclip", "-selection", sel, "-i", "-verbose")
	cmd.Stdin = strings.NewReader(text)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	owner := &xclipOwner{}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if n, ok := parseRequestNumber(sc.Text()); ok && n >= 1 {
				owner.served.Store(int64(n - 1))
				owner.mu.Lock()
				owner.times = append(owner.times, time.Now())
				owner.mu.Unlock()
			}
		}
		_ = cmd.Wait()
	}()
	return owner, nil
}

// restoreAttempts is how many times Restore re-publishes the saved
// clipboard when a verification read shows something else still owns it.
// One retry covers the ordinary handoff race (a desktop clipboard manager
// grabbing the selection as our dictation owner dies); beyond that we are
// fighting a determined owner and should not spin.
const restoreAttempts = 2

// FetchTimes returns when the current publication was read, newest last.
// Empty when nothing has read it (or nothing was published).
func (c *Clipboard) FetchTimes() []time.Time {
	c.mu.Lock()
	owner := c.owner
	c.mu.Unlock()
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return append([]time.Time(nil), owner.times...)
}

// ServesText reports whether the clipboard right now serves exactly this
// text — that is, our own publication is still the live selection and
// nobody claimed it back. Cheap: one read.
func (c *Clipboard) ServesText(text string) bool {
	out, err := xclipOutput("-selection", "clipboard", "-o")
	return err == nil && string(out) == text
}

func (c *Clipboard) Restore(s Saved) error {
	// The dictation owner is being replaced — drop it so a late WaitPasted
	// can't attribute the new owner's fetches to a paste that is over.
	c.mu.Lock()
	c.owner = nil
	c.mu.Unlock()

	for attempt := 1; ; attempt++ {
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
		// Publishing a selection is a claim, not a guarantee: anyone can
		// claim it right back. Read it back and re-publish once if the
		// user's content did not actually land — losing the clipboard to
		// a dictation is exactly what this whole dance exists to prevent.
		if restoredOK(s) {
			break
		}
		if attempt >= restoreAttempts {
			log.Printf("clipboard: restored content did not stick after %d attempts; another owner is claiming the selection", attempt)
			break
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

// restoreSettleDelay is how long Restore lets the selection settle before
// reading it back. A fresh xclip claims within a few ms; the rest of the
// window is there to catch a competing owner (a clipboard manager
// preserving the dying dictation owner's content) claiming it right back.
// It costs nothing the user waits on — the text is already inserted by
// the time Restore runs.
const restoreSettleDelay = 150 * time.Millisecond

// restoredOK reports whether the clipboard really serves the saved payload
// once the dust has settled. Deliberately reads AFTER the delay rather than
// returning on the first match: a claim landing a moment later is exactly
// the case worth catching. Text is compared exactly; binary payloads are
// checked by the advertised target only, since re-reading a multi-megabyte
// image just to compare it would cost more than the whole insert.
func restoredOK(s Saved) bool {
	if !s.HasContent {
		// Nothing to protect — an empty clipboard is the goal state.
		return true
	}
	time.Sleep(restoreSettleDelay)
	if s.Target != "" {
		targets, _ := readTargets("clipboard")
		return containsTarget(targets, s.Target)
	}
	out, err := xclipOutput("-selection", "clipboard", "-o")
	return err == nil && string(out) == s.Text
}

func containsTarget(targets []string, want string) bool {
	for _, t := range targets {
		if t == want {
			return true
		}
	}
	return false
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
