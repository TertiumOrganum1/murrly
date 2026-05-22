//go:build !darwin

// Package dockmenu is a no-op outside macOS — only macOS has a Dock.
package dockmenu

func Install(onCopy func(int), onToggleAutostart, onOpenConfig, onQuit func()) {}
func SetTranscripts(latest, previous, older string)                            {}
func SetAutostart(enabled bool)                                                {}
