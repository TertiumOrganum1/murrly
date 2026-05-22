//go:build !darwin

// Package dockmenu is a no-op outside macOS — only macOS has a Dock.
package dockmenu

func Install(onCopy, onPickModel func(int), onToggleAutostart, onOpenConfig, onQuit func(), modelLabels []string) {
}
func SetTranscripts(latest, previous, older string) {}
func SetAutostart(enabled bool)                     {}
func SetActiveModel(index int)                      {}
