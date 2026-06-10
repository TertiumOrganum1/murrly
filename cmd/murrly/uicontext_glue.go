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

func stashUIContext() {
	if !uictxActive {
		return
	}
	c := uicontext.Capture()
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

// adjustTextForContext is the app.Config.AdjustText hook: read the
// focused field around the insertion point and fit the transcription
// to it (uicontext.Apply's rules). Runs on the App goroutine right
// before the clipboard dance, i.e. while the target window still owns
// the focus on every hotkey path.
func adjustTextForContext(text string) string {
	c, ok := takeUIContext()
	if !ok {
		c = uicontext.Capture()
	}
	out := uicontext.Apply(text, c)
	if out != text {
		log.Printf("uicontext: %s preceding=%q space=%v following=%q atEnd=%v: %q -> %q",
			c.Status, c.Preceding, c.SpaceBefore, c.Following, c.AtEnd, text, out)
	} else {
		log.Printf("uicontext: %s (no change)", c.Status)
	}
	return out
}
