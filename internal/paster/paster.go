// Package paster sends a "paste" keystroke to the focused window.
package paster

import (
	"sync"
	"time"
)

type Paster struct{}

func New() *Paster { return &Paster{} }

// The synthetic Ctrl+V races the physical push-to-talk key still going up:
// catch the release mid-flight and the target sees the V without the Ctrl —
// a literal "v" dropped into the user's text. The guard against it is a wait.
//
// The wait belongs to the KEY RELEASE, though, not to the paste. A dictation
// spends the whole transcription between the two, so by the time it asks to
// paste there is nothing left to race and nothing to wait for — the flat sleep
// that used to sit there was a fifth of a second of the user watching nothing
// happen, on every single insert. Only the paths that paste right after a key
// (paste-last) still sleep, and only for the part of the window that is
// genuinely still ahead.
var lastKeyUp struct {
	mu sync.Mutex
	at time.Time
}

// NoteKeyUp records the moment the push-to-talk key was released. Called by
// whoever pumps the hotkey events; if nobody does, settleRemaining simply
// returns the full window and behaviour is the old one.
func NoteKeyUp() {
	lastKeyUp.mu.Lock()
	defer lastKeyUp.mu.Unlock()
	lastKeyUp.at = time.Now()
}

// settleRemaining is how much of window has not already elapsed since the last
// key release.
func settleRemaining(window time.Duration) time.Duration {
	lastKeyUp.mu.Lock()
	at := lastKeyUp.at
	lastKeyUp.mu.Unlock()
	if at.IsZero() {
		return window
	}
	if left := window - time.Since(at); left > 0 {
		return left
	}
	return 0
}
