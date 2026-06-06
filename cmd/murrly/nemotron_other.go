//go:build !linux

package main

import "github.com/tertiumorganum1/murrly/internal/app"

// setupNemotron is a no-op on non-Linux platforms: the Nemotron engine
// (NeMo/CUDA sidecar) ships for Linux only. Returns nil so the app keeps the
// legacy Whisper-only path and leaves the Break keys unwired.
func setupNemotron(events chan app.Event, whisper app.Transcriber, whisperMulti app.MultiTranscriber) app.CrossEngine {
	return nil
}
