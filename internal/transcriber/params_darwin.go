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

// platformReuseContext is true on macOS: we keep a single
// whisper.Context alive for the lifetime of the Transcriber and
// reuse it across every Transcribe call. Allocating Metal compute
// buffers on each push-to-talk added ~1 s of latency on M1 Pro, so
// caching the context is a meaningful UX win on this platform.
//
// Linux is fine creating a fresh context per call — the upstream
// pre-macOS code did exactly that and it's the configuration that
// the user reports as the most stable.
func platformReuseContext() bool { return true }

// platformFastModeRetry is true on macOS: when transcribe output
// looks like Whisper's degraded "fast-mode" output (long enough, no
// caps, no punctuation), retry the whole audio with leading silence
// padding + beam=5. With the macOS default beam=1 greedy decoder
// this recovery path actually fires usefully.
//
// On Linux (beam=5 by default, beam_search already escapes most
// attractors on its own) the retry was observed to be a net
// negative: it would replace acceptable output with a beam=5
// re-decode that sometimes itself produced fast-mode-shaped text,
// and we'd accept that as "recovered" and overwrite. Pre-Mac code
// didn't have any retry at all, and the user reports that was the
// most stable state.
func platformFastModeRetry() bool { return true }
