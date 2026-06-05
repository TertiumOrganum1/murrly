//go:build darwin

package config

// defaultBeamSize on macOS is 1 — Metal on Apple Silicon noticeably
// slows down past greedy decode, and short push-to-talk clips don't
// usually need beam_search. Users who want adaptive bumping on long
// clips can set whisper.adaptive = true in config.toml.
func defaultBeamSize() int { return 1 }

// defaultMultiInferenceCount on macOS is 2 — two sequential variants per
// recording: the best is auto-inserted and Ctrl+F11 lets the user pick
// between them. Kept at 2 (vs Linux's 4) because Whisper is slower on
// Metal than on CUDA, and each variant is a full sequential pass — 2×
// large-v3-turbo (~1.5 s floor each) stays within a tolerable ~3 s.
func defaultMultiInferenceCount() int { return 2 }
