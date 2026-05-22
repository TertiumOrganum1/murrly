//go:build darwin

package config

// defaultBeamSize on macOS is 1 — Metal on Apple Silicon noticeably
// slows down past greedy decode, and short push-to-talk clips don't
// usually need beam_search. Users who want adaptive bumping on long
// clips can set whisper.adaptive = true in config.toml.
func defaultBeamSize() int { return 1 }
