//go:build !darwin && !linux

package uicontext

// Capture is a no-op outside macOS (AX API) and Linux (AT-SPI).
// Returning HasContext=false makes Apply pass the text through
// unchanged.
func Capture() Context {
	return Context{Status: "unsupported-platform"}
}
