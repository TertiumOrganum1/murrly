//go:build !linux

package main

import (
	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/menuactions"
)

// setupNemotron is a no-op on non-Linux platforms: the Nemotron engine
// (NeMo/CUDA sidecar) ships for Linux only. Returns nil so the app keeps the
// legacy Whisper-only path and leaves the Break keys unwired.
func setupNemotron(events chan app.Event, ncfg config.NemotronConfig) app.NemotronEngine {
	return nil
}

// wireNemotronStatus is a no-op off Linux (no Nemotron menu group).
func wireNemotronStatus(actions *menuactions.Actions, eng app.NemotronEngine) {}
