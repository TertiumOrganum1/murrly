//go:build !linux

package main

import "github.com/tertiumorganum1/murrly/internal/app"

// setupNemotron is a no-op on non-Linux platforms: the Nemotron engine
// (NeMo/CUDA sidecar) ships for Linux only. Returns nil so the app leaves
// the Break path unwired.
func setupNemotron(events chan app.Event) app.Transcriber { return nil }
