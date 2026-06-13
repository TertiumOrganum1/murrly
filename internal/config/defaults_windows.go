//go:build windows

package config

// defaultBeamSize on Windows is 5 — same as Linux. The target machine is a
// CUDA box (RTX 4090); beam=5 keeps full punctuation on long dictations and
// costs well under a second per call on that GPU.
func defaultBeamSize() int { return 5 }

// defaultMultiInferenceCount on Windows is 4 — same as Linux. The 4090 has
// VRAM headroom for several large-v3 contexts (shared weights + ~0.6 GB per
// context), so 4 parallel variants give a useful spread to score and pick.
func defaultMultiInferenceCount() int { return 4 }
