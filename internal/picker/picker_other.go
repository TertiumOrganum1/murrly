//go:build !linux

package picker

// Pick is a no-op outside Linux — the zenity/X11 picker isn't available.
// macOS multi-inference selection (e.g. via an osascript dialog) is
// future work; until then Alt+F12 simply does nothing there.
func Pick(text string, options []string) (int, bool) {
	return 0, false
}
