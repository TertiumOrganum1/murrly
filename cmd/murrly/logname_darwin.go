//go:build darwin

package main

// logAppName matches the Apple convention used by the paths package on macOS:
// ~/Library/Caches/Murrly/ (capital M).
func logAppName() string { return "Murrly" }
