//go:build linux

package transcriber

import whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"

// applyPlatformWhisperParams is a no-op on Linux. With beam_size=5
// (the Linux config default) the decoder is already protected
// against attractor lock-in by beam search, and whisper.cpp's
// upstream-default max_context=-1 (unlimited carry-over) keeps the
// language model grounded across 30 s window boundaries — pinning
// max_context=0 here was observed to make output worse (more
// temperature-fallback firings, more fast-mode-shaped output).
// See params_darwin.go for the macOS rationale.
func applyPlatformWhisperParams(ctx whisper.Context) {}
