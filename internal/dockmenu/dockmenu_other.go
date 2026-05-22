//go:build !darwin

// Package dockmenu is a no-op outside macOS — only macOS has a Dock.
package dockmenu

import "github.com/tertiumorganum1/murrly/internal/menuactions"

func Install(*menuactions.Actions)                  {}
func SetTranscripts(latest, previous, older string) {}
func SetAutostart(enabled bool)                     {}
func SetActiveModel(index int)                      {}
