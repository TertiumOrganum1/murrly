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
// medium and small are here for the processor, where the encoder is the whole
// wait and it scales with the model — on a 5950X, 11 s of speech: turbo 5.3 s,
// medium 3.7 s, small 1.0 s. On a card the difference is not worth the quality.
var Available = []string{
	"large-v3-turbo",
	"large-v3-turbo-q5_0",
	"medium-q5_0",
	"small-q5_1",
	Parakeet,
}

// Parakeet is the odd one in the list: not a Whisper file at all but a NeMo
// transducer run through sherpa-onnx, kept in the same menu because from the
// user's side it is the same choice — which engine writes the words. It lives
// in a directory of ONNX graphs rather than a ggml-<name>.bin, so anything that
// tests a model for presence has to special-case it.
const Parakeet = "parakeet-v3"

// Labels are short user-visible descriptions for each model — paired with
// Available by index.
var Labels = map[string]string{
	"large-v3-turbo":      "large-v3-turbo (1.5 ГБ, быстрее)",
	"large-v3-turbo-q5_0": "large-v3-turbo-q5_0 (550 МБ, быстро, бытовая речь)",
	"medium-q5_0":         "medium-q5_0 (540 МБ, для процессора)",
	"small-q5_1":          "small-q5_1 (180 МБ, самая быстрая, путает язык)",
	Parakeet:              "parakeet-v3 (620 МБ, процессор, без видеокарты)",
}
