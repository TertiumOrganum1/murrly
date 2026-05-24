//go:build darwin

package transcriber

import whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"

// applyPlatformWhisperParams sets context options that improve
// behaviour on this specific platform.
//
// On macOS the default beam_size is 1 (greedy decode), which is more
// prone to falling into the kind of low-entropy attractor that
// produces stutter loops. The default max_context=-1 (unlimited
// carry-over) then propagates a loop that arose in segment 1 into
// segments 2..N — once Whisper falls into "Это не так просто."
// (or similar) the prompt for every following 30 s window carries
// the same text and naturally continues the loop.
//
// Pinning max_context=0 breaks that propagation: each 30 s window
// decodes with a fresh prompt. A stutter is confined to at most one
// window; the rest of the utterance gets a chance to decode cleanly.
//
// On Linux, where beam_size defaults to 5, beam search already
// guards against attractor lock-in (multiple parallel hypotheses
// give the decoder ways to escape a loop), and unlimited carry-over
// is the helpful upstream-tested default. The Linux build of this
// helper is a no-op — see params_linux.go.
func applyPlatformWhisperParams(ctx whisper.Context) {
	ctx.SetMaxContext(0)
}
