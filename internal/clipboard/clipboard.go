// Package clipboard saves and restores the system clipboard text.
// MVP: plain text only.
package clipboard

type Saved struct {
	Text       string
	HasContent bool
	Primary    string // X11 primary selection; macOS leaves empty.
	HasPrimary bool   // X11 primary selection; macOS leaves false.
	// platformState carries an opaque per-platform handle. On macOS it
	// holds a CF-retained snapshot of every NSPasteboardItem (text +
	// image + RTF + file URLs etc.) so Restore puts back the user's
	// previous clipboard exactly — including non-text content. On Linux
	// it's unused.
	platformState uintptr
}

type Clipboard struct {
	RestorePrimary bool
}

func New() *Clipboard {
	return &Clipboard{RestorePrimary: true}
}
