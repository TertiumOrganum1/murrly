// Package clipboard saves and restores the system clipboard text.
// MVP: plain text only.
package clipboard

type Saved struct {
	Text       string
	HasContent bool
	Primary    string // X11 primary selection; macOS leaves empty.
	HasPrimary bool   // X11 primary selection; macOS leaves false.
}

type Clipboard struct {
	RestorePrimary bool
}

func New() *Clipboard {
	return &Clipboard{RestorePrimary: true}
}
