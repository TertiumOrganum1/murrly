//go:build !darwin && !linux && !windows

package uicontext

// Capture is a no-op outside macOS (AX API), Linux (AT-SPI) and Windows
// (UI Automation). Returning HasContext=false makes Apply pass the text
// through unchanged.
func Capture() Context {
	return Context{Status: "unsupported-platform"}
}
