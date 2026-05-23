//go:build !darwin

package uicontext

// Capture is a no-op outside macOS — Linux X11 / Wayland have no
// equivalent of AXUIElementCopyAttributeValue that would let us read
// text out of the focused field. Returning HasContext=false makes
// Apply pass the text through unchanged.
func Capture() Context {
	return Context{Status: "unsupported-platform"}
}
