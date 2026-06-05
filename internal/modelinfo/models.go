// Package modelinfo lists the Whisper models Murrly knows how to pick
// from in the menu UI. Shared between the dock menu (macOS) and the
// tray menu (both platforms).
package modelinfo

// Available models, ordered from heaviest (best quality) to lightest
// (fastest). Filenames follow the whisper.cpp convention ggml-<name>.bin.
//
// large-v3 (the non-turbo 3 GB model) was dropped: on Apple Silicon it
// was the slowest to load and run, and in practice produced unstable
// output (stutter loops, fast-mode) more often than the turbo variants
// without a quality win worth its cost. The turbo line is the supported set.
var Available = []string{
	"large-v3-turbo",
	"large-v3-turbo-q5_0",
}

// Labels are short user-visible descriptions for each model — paired with
// Available by index.
var Labels = map[string]string{
	"large-v3-turbo":      "large-v3-turbo (1.5 ГБ, быстрее)",
	"large-v3-turbo-q5_0": "large-v3-turbo-q5_0 (550 МБ, быстро, бытовая речь)",
}
