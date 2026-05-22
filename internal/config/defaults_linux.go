//go:build linux

package config

// defaultBeamSize on Linux is 2 — Whisper switches from greedy to
// beam_search at this width, which keeps punctuation and capitalisation
// intact on long clips. CUDA + large-v3 handles beam=2 in well under a
// second even for 40+ s recordings, so the cost is negligible.
func defaultBeamSize() int { return 2 }
