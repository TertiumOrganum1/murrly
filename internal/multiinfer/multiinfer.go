// Package multiinfer runs several Whisper inferences over the same audio
// — each perturbed by a different leading-silence shift — then scores
// and ranks the results so the best can be inserted and the rest offered
// to the user. It's the engine behind the Linux multi-inference feature
// (config: whisper.multi_inference_count).
//
// The passes run SEQUENTIALLY on a single reused context. Concurrent
// whisper_full on multiple contexts of one model crashes the ggml-CUDA
// backend (its global device/cuBLAS state isn't safe for parallel use),
// and on a single GPU the encoder work serializes anyway. One context
// reused N times keeps VRAM flat (model + one context, ~independent of
// count) and is the only stable option.
package multiinfer

import (
	"sort"
	"sync"

	"github.com/tertiumorganum1/murrly/internal/transcriber"
)

const (
	pcmSampleRateHz = 16000
	// padLeadBaseSec — leading silence on every variant, including the
	// first. Variant 1 runs with just this baseline (the "as-is" pass);
	// later variants add padStepSec on top of it.
	padLeadBaseSec = 1.25
	// padStepSec — each further variant i adds i*padStepSec seconds of
	// leading silence beyond the baseline (so variants run with 1.25,
	// 2.75, 4.25, 5.75, … s of lead). Shifting speech within Whisper's
	// 30 s window changes how it aligns and decodes, which is what gives
	// the variants a chance to differ; a wider step spreads them more.
	padStepSec = 1.5
	// padTrailSec — fixed trailing pad on every variant. Guards the last
	// word from the chunk boundary; identical across variants so it
	// doesn't itself reduce diversity.
	padTrailSec = 1.0
)

// Candidate is one inference result with its scores.
type Candidate struct {
	Index      int     // original variant index (0-based, pre-ranking)
	Text       string  // post-processed transcription
	Confidence float64 // Whisper mean-token probability [0,1]
	Score      float64 // combined rank used for ordering [0,1]
	PadLeadSec float64 // leading silence this variant ran with
}

// Runner owns the model and a single reused inference context. Build
// once at startup and reuse across recordings. Reload swaps in a
// different model (for the tray model-picker) without disturbing count.
type Runner struct {
	mu        sync.Mutex
	model     *transcriber.Model
	session   *transcriber.Session
	cfg       transcriber.Config
	count     int
	scoreMode ScoreMode
}

// New loads the model once and allocates a single session. count is how
// many sequential variants Run produces per recording (>= 1). scoreMode
// selects how variants are ranked (combined / confidence / heuristic);
// it can be changed live with SetScoreMode.
func New(cfg transcriber.Config, count int, scoreMode ScoreMode) (*Runner, error) {
	if count < 1 {
		count = 1
	}
	m, s, err := openModelSession(cfg)
	if err != nil {
		return nil, err
	}
	return &Runner{model: m, session: s, cfg: cfg, count: count, scoreMode: scoreMode}, nil
}

// SetScoreMode changes how subsequent Run batches are ranked. Takes the
// batch lock so it can't race a Run mid-flight; already-cached variants
// keep the score they were ranked with (the new mode applies to the next
// recording or Ctrl+F12 batch).
func (r *Runner) SetScoreMode(mode ScoreMode) {
	r.mu.Lock()
	r.scoreMode = mode
	r.mu.Unlock()
}

func openModelSession(cfg transcriber.Config) (*transcriber.Model, *transcriber.Session, error) {
	m, err := transcriber.OpenModel(cfg.ModelPath)
	if err != nil {
		return nil, nil, err
	}
	s, err := m.NewSession(cfg)
	if err != nil {
		_ = m.Close()
		return nil, nil, err
	}
	return m, s, nil
}

// Reload swaps to a model at modelPath, carrying over the rest of the
// current config. Used by the tray model picker in multi-inference mode.
func (r *Runner) Reload(modelPath string) error {
	cfg := r.cfg
	cfg.ModelPath = modelPath
	return r.ReloadConfig(cfg)
}

// ReloadConfig rebuilds the model+session from a fresh transcriber
// config (model, beam, prompt, …), keeping the variant count. Blocks any
// in-flight Run until the swap completes; the old model is released
// afterward. Used by the tray "reload config" action in multi mode.
func (r *Runner) ReloadConfig(cfg transcriber.Config) error {
	m, s, err := openModelSession(cfg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	old := r.model
	r.model, r.session, r.cfg = m, s, cfg
	r.mu.Unlock()
	return old.Close()
}

// Count is the number of variants this runner produces per recording.
func (r *Runner) Count() int { return r.count }

// Close releases the model and its session.
func (r *Runner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.model.Close()
}

// Run produces count variants of the same PCM — each with a different
// leading-silence shift — by running the single context sequentially,
// then scores and ranks them best-first. Failed or empty variants are
// dropped. Returns nil only if every variant failed.
//
// leadOffsetSec is added to every variant's leading silence on top of
// its per-index step, so successive reprocess batches explore fresh
// chunk alignments instead of repeating the first batch.
//
// Sequential by necessity: see the package doc — concurrent contexts
// crash ggml-CUDA, and the GPU serializes the heavy work regardless.
func (r *Runner) Run(pcm []float32, leadOffsetSec float64) []Candidate {
	// Hold the lock for the whole batch so a concurrent Reload (model
	// switch from the menu) waits until this recording's variants are
	// done rather than freeing the model mid-inference.
	r.mu.Lock()
	defer r.mu.Unlock()

	ranked := make([]Candidate, 0, r.count)
	for i := 0; i < r.count; i++ {
		lead := leadOffsetSec + float64(i)*padStepSec
		padded := padPCM(pcm, lead, padTrailSec)
		text, conf, err := r.session.Run(padded)
		if err != nil || text == "" {
			continue
		}
		ranked = append(ranked, Candidate{
			Index:      i,
			Text:       text,
			Confidence: conf,
			Score:      score(text, conf, r.scoreMode),
			PadLeadSec: lead,
		})
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		return ranked[a].Score > ranked[b].Score
	})
	return ranked
}

// RunOne does a single inference pass over the PCM as-is — no leading-
// silence perturbation, no variant batch, no scoring — and returns the
// post-processed text. It backs the "multi-inference off" path: the same
// model/session the variant batch uses, so toggling the feature live costs
// nothing (no second model load). Caller-side padding (pad_silence /
// reprocess silence) is applied before this, exactly as in the single-pass
// engine.
func (r *Runner) RunOne(pcm []float32) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	text, _, err := r.session.Run(pcm)
	return text, err
}

// padPCM returns a new buffer with startSec of zero samples prepended
// and endSec appended. Either may be 0.
func padPCM(pcm []float32, startSec, endSec float64) []float32 {
	start := int(startSec * pcmSampleRateHz)
	end := int(endSec * pcmSampleRateHz)
	out := make([]float32, start+len(pcm)+end)
	copy(out[start:], pcm)
	return out
}
