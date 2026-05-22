//go:build linux

package config

// defaultBeamSize on Linux is 5 — matches upstream whisper-cli's
// beam_search default. Width 2-3 turned out to be too narrow on long
// dictations (Whisper still occasionally dropped punctuation in
// observed transcripts); 5 reliably keeps full formatting. CUDA +
// large-v3 handles beam=5 in well under a second per call, so the
// cost is negligible.
func defaultBeamSize() int { return 5 }
