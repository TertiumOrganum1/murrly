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

// platformReuseContext is false on Linux: we go back to the pre-macOS
// behaviour of creating a fresh whisper.Context inside every
// Transcribe call. The user reports this matches the most stable
// known state of the project, and on CUDA + large-v3 the context-
// allocation overhead is negligible.
func platformReuseContext() bool { return false }

// platformFastModeRetry is false on Linux: with beam=5 beam search
// the looksLikeFastMode retry (silence-padded re-decode at beam=5)
// is essentially a no-op at best and a regression at worst — it
// occasionally replaced acceptable first-pass output with a worse
// retry result. We just keep the first-pass text and let the
// post-processing pipeline (collapseRepeatedBlocks,
// dedupeSubstantialSentences) handle the rare stutter that slips
// through.
func platformFastModeRetry() bool { return false }
