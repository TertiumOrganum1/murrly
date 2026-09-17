// Package parakeet is the second recogniser: a NeMo Parakeet TDT transducer
// run through sherpa-onnx instead of whisper.cpp.
//
// It exists for the processor. Whisper encodes a fixed 30 s window whatever the
// clip holds, so a two-second phrase costs the same as a thirty-second one and
// on a CPU that cost is the whole wait — measured on a 5950X: ~3.7 s for a
// medium model per phrase, however short the phrase. A transducer processes the
// audio it was actually given, so the wait scales with what was said. That is
// the structural difference, not a tuning one.
//
// Everything above the recogniser is unchanged: the text comes back through
// transcriber.FilterText, so the same repeat-collapse, filler-strip,
// capitalisation and terminal punctuation apply as to a Whisper result, and the
// profanity filter and insertion routes downstream cannot tell which engine
// produced the phrase.
package parakeet

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/tertiumorganum1/murrly/internal/modelinfo"
	"github.com/tertiumorganum1/murrly/internal/paths"
	"github.com/tertiumorganum1/murrly/internal/transcriber"
)

// ModelName is how the model picker and config.toml refer to this engine. It is
// deliberately shaped like the Whisper short names — the menu is one list, and
// the user picks either a Whisper file or this.
const ModelName = modelinfo.Parakeet

const (
	// sampleRateHz is what the recorder feeds us. sherpa-onnx resamples when
	// it differs, but it never does here.
	sampleRateHz = 16000
	// featureDim is the number of mel bins the v3 encoder was trained on.
	// Whisper's 80 is the more familiar number; the NeMo models use 128, and
	// getting it wrong does not fail loudly — it feeds the encoder a feature
	// vector of the wrong shape and the text comes back as noise.
	featureDim = 128
	// modelType short-circuits sherpa-onnx's model sniffing at load time.
	modelType = "nemo_transducer"
	// minSamples pads out a clip too short to make a single encoder frame.
	// AcceptWaveform is given a raw pointer into the slice, so an empty one
	// is a nil dereference in C, and a handful of samples is a crash further
	// in; a tenth of a second is cheap and always safe.
	minSamples = sampleRateHz / 10
)

// Recognizer holds the loaded ONNX graphs. One at a time: Transcribe
// serialises on the mutex the way the Whisper transcriber does, because the
// stream is created per call but the recogniser behind it is shared.
type Recognizer struct {
	mu  sync.Mutex
	rec *sherpa.OfflineRecognizer
	dir string
}

// Dir is where the model files live: <ModelsDir>/parakeet-v3/.
func Dir() (string, error) {
	dir, err := paths.ModelsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ModelName), nil
}

// Present reports whether the model is downloaded and complete. The model
// picker uses it the way it uses a stat on ggml-<name>.bin — a model that is
// not there is not offered.
func Present() bool {
	dir, err := Dir()
	if err != nil {
		return false
	}
	_, err = resolveFiles(dir)
	return err == nil
}

// files are the four paths sherpa-onnx needs.
type files struct {
	encoder, decoder, joiner, tokens string
}

// resolveFiles finds the graphs by glob rather than by exact name: the release
// archive ships them as encoder.int8.onnx (the quantized build) and a
// float build would name them encoder.onnx, and which one the user unpacked is
// not this package's business.
func resolveFiles(dir string) (files, error) {
	var f files
	for _, part := range []struct {
		glob string
		dst  *string
	}{
		{"encoder*.onnx", &f.encoder},
		{"decoder*.onnx", &f.decoder},
		{"joiner*.onnx", &f.joiner},
	} {
		matches, err := filepath.Glob(filepath.Join(dir, part.glob))
		if err != nil || len(matches) == 0 {
			return files{}, fmt.Errorf("%s: нет файла %s", dir, part.glob)
		}
		*part.dst = matches[0]
	}
	f.tokens = filepath.Join(dir, "tokens.txt")
	if _, err := os.Stat(f.tokens); err != nil {
		return files{}, fmt.Errorf("%s: нет tokens.txt", dir)
	}
	return f, nil
}

// New loads the model. threads <= 0 means one per physical core, the same
// answer the Whisper CPU path uses — the two engines run on the same processor
// and an SMT-wide thread count hurts both alike.
func New(threads int) (*Recognizer, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	f, err := resolveFiles(dir)
	if err != nil {
		return nil, err
	}
	if threads <= 0 {
		threads = transcriber.CPUThreads()
	}

	cfg := sherpa.OfflineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: sampleRateHz,
			FeatureDim: featureDim,
		},
		ModelConfig: sherpa.OfflineModelConfig{
			Transducer: sherpa.OfflineTransducerModelConfig{
				Encoder: f.encoder,
				Decoder: f.decoder,
				Joiner:  f.joiner,
			},
			Tokens:     f.tokens,
			NumThreads: threads,
			ModelType:  modelType,
			Provider:   "cpu",
		},
		// greedy_search, not modified_beam_search: on a transducer the beam
		// buys little and costs a multiple of the decode, and the decode is
		// the part we came here to keep short.
		DecodingMethod: "greedy_search",
	}

	t0 := time.Now()
	rec := sherpa.NewOfflineRecognizer(&cfg)
	if rec == nil {
		return nil, fmt.Errorf("parakeet: не удалось загрузить модель из %s", dir)
	}
	log.Printf("parakeet: модель загружена из %s за %dms (%d потоков)",
		filepath.Base(dir), time.Since(t0).Milliseconds(), threads)
	return &Recognizer{rec: rec, dir: dir}, nil
}

// Transcribe implements app.Transcriber — same signature as the Whisper side,
// so the engine switch upstream can hand the dictation to either.
func (r *Recognizer) Transcribe(pcm []float32) (string, error) {
	if len(pcm) == 0 {
		return "", nil
	}
	if len(pcm) < minSamples {
		padded := make([]float32, minSamples)
		copy(padded, pcm)
		pcm = padded
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rec == nil {
		return "", fmt.Errorf("parakeet: модель выгружена")
	}

	stream := sherpa.NewOfflineStream(r.rec)
	defer sherpa.DeleteOfflineStream(stream)

	t0 := time.Now()
	stream.AcceptWaveform(sampleRateHz, pcm)
	r.rec.Decode(stream)
	raw := stream.GetResult().Text

	audioSec := float64(len(pcm)) / sampleRateHz
	elapsed := time.Since(t0)
	log.Printf("parakeet: %.1fs аудио за %dms (rtf %.2f)",
		audioSec, elapsed.Milliseconds(), elapsed.Seconds()/audioSec)

	formatted := transcriber.FilterText(raw)
	if raw != formatted {
		log.Printf("parakeet: raw=%q formatted=%q", raw, formatted)
	}
	return formatted, nil
}

// Close frees the ONNX session. Idempotent — the engine switch may release the
// recogniser and then hit the shutdown path over the same pointer.
func (r *Recognizer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rec == nil {
		return nil
	}
	sherpa.DeleteOfflineRecognizer(r.rec)
	r.rec = nil
	return nil
}
