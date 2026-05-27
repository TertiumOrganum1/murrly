//go:build linux

package config

// defaultBeamSize on Linux is 5 — matches upstream whisper-cli's
// beam_search default. Width 2-3 turned out to be too narrow on long
// dictations (Whisper still occasionally dropped punctuation in
// observed transcripts); 5 reliably keeps full formatting. CUDA +
// large-v3 handles beam=5 in well under a second per call, so the
// cost is negligible.
func defaultBeamSize() int { return 5 }

// defaultMultiInferenceCount on Linux is 4 — the 4090 has VRAM headroom
// for several large-v3 contexts (shared weights + ~0.6 GB per context),
// and 4 parallel variants give a useful spread to score and pick from.
func defaultMultiInferenceCount() int { return 4 }
