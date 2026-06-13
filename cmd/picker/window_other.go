//go:build !linux && !windows

package main

// arrangeWindow is a no-op on platforms without a window-placement backend
// (macOS): Fyne centres the splash window on the frontmost screen by default,
// which is the historical behaviour there.
func arrangeWindow() {}
