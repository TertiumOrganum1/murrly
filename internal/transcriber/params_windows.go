//go:build windows

package transcriber

import whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"

// Windows mirrors the Linux/CUDA path exactly (the target is an RTX 4090):
// beam_size=5 protects against attractor lock-in, upstream max_context=-1
// keeps the language model grounded across window boundaries, a fresh
// context per call is cheap on CUDA, and the fast-mode retry is a net
// regression with beam search. See params_linux.go for the full rationale.
func applyPlatformWhisperParams(ctx whisper.Context) {}

func platformReuseContext() bool { return false }

func platformFastModeRetry() bool { return false }
