//go:build !darwin

// Package overlay is a no-op outside macOS. The translucent status pill
// exists to compensate for the M-series MacBook notch hiding the menu
// bar tray icon — Linux/X11 doesn't have that problem, and the
// existing tray icon stays the canonical state indicator there.
package overlay

func Show(text string) {}
func Hide()             {}
