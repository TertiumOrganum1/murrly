// Package clipboard puts text in the system clipboard for the time it takes
// to paste it, and keeps whatever was there before in our own memory.
//
// The X11 side is the one with teeth — see clipboard_linux.go for why
// ownership is scoped to the paste rather than held for the session. macOS
// and Windows have a real clipboard store with no owning process, so there
// Publish is just a write and the release func does nothing.
package clipboard

import "sync"

// Saved is a snapshot of what the user had in the clipboard before a
// dictation displaced it: either text, or the raw bytes of a picture.
//
// Rich text is a deliberate omission. HTML never travels alone — the source
// app offers text/plain beside it — and handing back only one of the pair is
// how a clipboard ends up unreadable to whatever asks it for the other. Taking
// the plain text loses the formatting and nothing else, which is the right
// trade for dictating over a copied fragment of Markdown.
//
// A picture is different: a screenshot in the clipboard IS image-only, so
// keeping the bytes loses nothing. They never go back into the clipboard
// though — see SaveImage in the app layer for why the way out is a file.
type Saved struct {
	// Text is what the clipboard held, empty when it held a picture.
	Text string
	// HasContent — this snapshot holds something worth offering back. A
	// snapshot with neither text nor image is not a snapshot.
	HasContent bool
	// Image is the raw encoded picture (PNG bytes and the like) and
	// ImageTarget the X11 selection target it came from, which doubles as
	// its MIME type and so names the file extension to save it under.
	Image       []byte
	ImageTarget string
}

// HasText reports whether the snapshot can go back into the clipboard. Only
// text ever does.
func (s Saved) HasText() bool { return s.Text != "" }

// HasImage reports whether the snapshot holds a picture, which the menu can
// only offer to write to a file.
func (s Saved) HasImage() bool { return len(s.Image) > 0 }

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

// Our own dictation is not "what the user had in the clipboard". Without this
// the stash eats itself: dictate once and the clipboard holds phrase A, so the
// snapshot taken at the start of the NEXT dictation stores A over the text the
// user had actually copied — one dictation and the safety net is gone.
//
// There is no "who put this here" field in any of the three clipboards (X11
// tracks an owning window, not an application identity, and by the time we are
// asked the owner is the desktop's clipboard manager, not us), so the marker
// is the text itself: remember verbatim what we published and refuse to
// snapshot it back. A user who copies, by hand, exactly the phrase they just
// dictated loses nothing real — the clipboard already holds it.
var (
	oursMu sync.Mutex
	ours   string
)

// markOurs records text as Murrly's own — called by Publish, the route that
// puts a dictation in the clipboard purely so Ctrl+V can carry it.
func markOurs(text string) {
	oursMu.Lock()
	defer oursMu.Unlock()
	ours = text
}

// forgetOurs drops the marker — called by Set, which is only ever reached
// because the user asked for something to BE in the clipboard (a menu item,
// paste-last, putting the displaced snapshot back). Content they chose is
// content worth snapshotting next time.
func forgetOurs() {
	oursMu.Lock()
	defer oursMu.Unlock()
	ours = ""
}

func isOurs(text string) bool {
	oursMu.Lock()
	defer oursMu.Unlock()
	return ours != "" && text == ours
}

type Clipboard struct{}

func New() *Clipboard { return &Clipboard{} }

// Snapshot copies what is in the clipboard into our own memory — unless what
// is in there is the dictation we published ourselves, in which case there is
// nothing of the user's left to save and the existing stash must survive.
//
// Best effort by design on every platform: an unresponsive owner, a format we
// do not keep, an empty clipboard all return ok=false and cost the caller
// nothing.
func (c *Clipboard) Snapshot() (Saved, bool) {
	s, ok := c.readSystemSnapshot()
	if !ok {
		return Saved{}, false
	}
	// Only text can be ours — Publish never puts a picture in the clipboard.
	if s.HasText() && isOurs(s.Text) {
		return Saved{}, false
	}
	return s, true
}
