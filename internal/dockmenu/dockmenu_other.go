//go:build !darwin

// Package dockmenu is a no-op outside macOS — only macOS has a Dock.
package dockmenu

func Install(onQuit, onOpenConfig, onCopyLatest func()) {}
