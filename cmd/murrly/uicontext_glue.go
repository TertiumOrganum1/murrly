package main

import (
	"log"
	"sync"
	"time"

	"github.com/tertiumorganum1/murrly/internal/uicontext"
)

// uictxActive gates the whole context-insert path — set in main() when
// the AdjustText hook is wired (Linux + output.context_insert).
var uictxActive bool

// pendingUICtx carries a Context captured BEFORE the picker window
// stole the focus. The Ctrl+F11 flow is the one insert path where
// "capture at insert time" reads the WRONG element (the picker's own
// UI), so pickerAdapter snapshots the target field first and
// adjustTextForContext consumes the snapshot after the pick.
var pendingUICtx struct {
	mu sync.Mutex
	c  uicontext.Context
	ok bool
	at time.Time
}

// captureWithTimeout runs uicontext.Capture under a HARD outer deadline.
// Capture already bounds its own D-Bus calls, but connecting to the a11y
// bus (address lookup + dial) is not itself guarded, and a wedged bus there
// would block the insert forever — leaving the tray stuck on "transcribing"
// because insertText never returns to set Idle. On overrun we abandon the
// (leaked) goroutine and treat the field as unreadable → text passes through
// untouched. Normal captures return in well under 100 ms, so no added
// latency in the common case.
func captureWithTimeout(d time.Duration) uicontext.Context {
	ch := make(chan uicontext.Context, 1)
	go func() { ch <- uicontext.Capture() }()
	select {
	case c := <-ch:
		return c
	case <-time.After(d):
		return uicontext.Context{Status: "capture-timeout"}
	}
}

func stashUIContext() {
	if !uictxActive {
		return
	}
	c := captureWithTimeout(1500 * time.Millisecond)
	pendingUICtx.mu.Lock()
	pendingUICtx.c, pendingUICtx.ok, pendingUICtx.at = c, true, time.Now()
	pendingUICtx.mu.Unlock()
}

func dropUIContext() {
	pendingUICtx.mu.Lock()
	pendingUICtx.ok = false
	pendingUICtx.mu.Unlock()
}

// takeUIContext returns the stashed snapshot at most once, and never a
// stale one (the minute guard covers a pick that was somehow neither
// consumed nor dropped).
func takeUIContext() (uicontext.Context, bool) {
	pendingUICtx.mu.Lock()
	defer pendingUICtx.mu.Unlock()
	if !pendingUICtx.ok || time.Since(pendingUICtx.at) > time.Minute {
		pendingUICtx.ok = false
		return uicontext.Context{}, false
	}
	pendingUICtx.ok = false
	return pendingUICtx.c, true
}

// adjustTextForcedMid is the app.Config.AdjustTextForced hook (Shift+F12):
// apply the mid-sentence transform unconditionally, with NO field reading —
// decapitalise the first letter, prepend one space, strip the phrase's
// terminal punctuation. Pure and platform-neutral (no Capture / AT-SPI).
func adjustTextForcedMid(text string) string {
	out := uicontext.Apply(text, uicontext.Context{HasContext: true, ForceMid: true})
	log.Printf("uicontext: forced-mid (Shift+F12): %q -> %q", text, out)
	return out
}

// adjustTextForContext is the app.Config.AdjustText hook: read the
// focused field around the insertion point and fit the transcription
// to it (uicontext.Apply's rules). Runs on the App goroutine right
// before the clipboard dance, i.e. while the target window still owns
// the focus on every hotkey path.
func adjustTextForContext(text string) string {
	c, ok := takeUIContext()
	if !ok {
		c = captureWithTimeout(1500 * time.Millisecond)
	}
	out := uicontext.Apply(text, c)
	log.Printf("uicontext: %s atStart=%v preceding=%q space=%v rightKnown=%v following=%q atEnd=%v: %q -> %q",
		c.Status, c.AtStart, c.Preceding, c.SpaceBefore, c.RightKnown, c.Following, c.AtEnd, text, out)
	return out
}
