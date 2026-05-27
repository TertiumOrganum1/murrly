//go:build darwin

package config

// defaultBeamSize on macOS is 1 — Metal on Apple Silicon noticeably
// slows down past greedy decode, and short push-to-talk clips don't
// usually need beam_search. Users who want adaptive bumping on long
// clips can set whisper.adaptive = true in config.toml.
func defaultBeamSize() int { return 1 }

// defaultMultiInferenceCount on macOS is 1 — single pass. Metal on
// Apple Silicon has tighter memory and weaker multi-context overlap;
// multi-inference is a Linux-first feature. Mac users can raise it.
func defaultMultiInferenceCount() int { return 1 }
