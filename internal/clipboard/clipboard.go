// Package clipboard saves and restores the system clipboard contents
// across the dictate-and-paste cycle. macOS preserves every NSPasteboard
// type via a CF-retained snapshot; Linux preserves text by default and
// the most-useful binary payload (typically image/png from screenshots)
// when the selection has non-text targets.
package clipboard

import "sync"

type Saved struct {
	Text       string
	HasContent bool
	Primary    string // X11 primary selection; macOS leaves empty.
	HasPrimary bool   // X11 primary selection; macOS leaves false.
	// Binary / Target — set on Linux when the clipboard owner advertises
	// a non-text MIME (image/png from a screenshot, image/jpeg, etc.).
	// Save reads the binary payload via `xclip -t <Target> -o`; Restore
	// re-publishes it via `xclip -t <Target> -i` so the original survives
	// the paste cycle intact. Empty on macOS — that path uses
	// platformState.
	Binary []byte
	Target string
	// platformState carries an opaque per-platform handle. On macOS it
	// holds a CF-retained snapshot of every NSPasteboardItem (text +
	// image + RTF + file URLs etc.) so Restore puts back the user's
	// previous clipboard exactly — including non-text content. On Linux
	// it's unused (Binary / Target cover the non-text case).
	platformState uintptr
}

// Stash holds the one clipboard snapshot taken before a replacing insert
// overwrote it, so the menu can offer it back. Exactly one slot: the point
// is undoing the last accident, not keeping a history — and a history of
// other applications' clipboards is a pile of passwords nobody asked us to
// collect. For the same reason it lives in memory only and is gone at exit.
type Stash struct {
	mu    sync.Mutex
	saved Saved
	has   bool
}

// Put replaces the stashed snapshot. Content-free snapshots are dropped:
// re-publishing an empty clipboard is not a restore, it is a wipe.
func (s *Stash) Put(v Saved) {
	if !v.HasContent {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved, s.has = v, true
}

// Peek returns the stashed snapshot without consuming it — the user may want
// it back more than once, and nothing else overwrites it until the next
// replacing insert.
func (s *Stash) Peek() (Saved, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved, s.has
}

type Clipboard struct {
	RestorePrimary bool
	// pasteTracker is platform-specific state for detecting when the
	// pasted text has actually been fetched by the target application
	// (see WaitPasted on Linux). Empty struct on platforms without an
	// observable selection owner.
	pasteTracker
}

func New() *Clipboard {
	return &Clipboard{RestorePrimary: true}
}
