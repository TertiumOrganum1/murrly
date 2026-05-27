// Package clipboard saves and restores the system clipboard contents
// across the dictate-and-paste cycle. macOS preserves every NSPasteboard
// type via a CF-retained snapshot; Linux preserves text by default and
// the most-useful binary payload (typically image/png from screenshots)
// when the selection has non-text targets.
package clipboard

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

type Clipboard struct {
	RestorePrimary bool
}

func New() *Clipboard {
	return &Clipboard{RestorePrimary: true}
}
