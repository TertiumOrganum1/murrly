// Package hotkey provides a global push-to-talk hotkey listener.
// The public API is platform-agnostic; Linux uses X11 (CGo) and
// macOS uses Carbon RegisterEventHotKey via golang.design/x/hotkey.
package hotkey

// Event is the kind of hotkey event delivered on Events().
type Event int

const (
	// EventDown fires when the hotkey is pressed.
	EventDown Event = iota
	// EventUp fires when the hotkey is released.
	EventUp
)
