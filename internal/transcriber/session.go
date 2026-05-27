package transcriber

import (
	"fmt"
	"strings"
	"sync"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

// Model is a loaded Whisper model, shareable across many Sessions.
// Sessions created from one Model share its weights in VRAM — only the
// per-session KV cache and compute buffers are duplicated. So N sessions
// cost roughly (model + N * per-context) rather than N * full, which is
// what makes multi-inference fit: large-v3 is ~3 GB of weights plus
// ~0.6 GB per context, so 8 sessions land near 8 GB, not 29 GB.
//
// This is a separate, narrower API from Transcriber (which bundles one
// model + one context + the fast-mode retry logic for the single-pass
// path). Sessions are the building block for internal/multiinfer.
type Model struct {
	m whisper.Model
}

// OpenModel loads the model weights into VRAM. Call Close to release.
func OpenModel(path string) (*Model, error) {
	m, err := whisper.New(path)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", path, err)
	}
	return &Model{m: m}, nil
}

// Close releases the model weights. Sessions created from it must not be
// used afterward.
func (mm *Model) Close() error { return mm.m.Close() }

// Session is a single inference context bound to a Model. Each holds its
// own decoder state, so distinct Sessions can run concurrently on
// different goroutines without sharing mutable inference state. (Whether
// the underlying ggml-CUDA backend truly overlaps the GPU work is a
// separate question — measured at runtime.)
type Session struct {
	ctx whisper.Context
	mu  sync.Mutex
}

// NewSession allocates and configures an inference context from the
// model. cfg supplies language, beam size, and initial prompt — the
// same configuration the single-pass Transcriber applies.
func (mm *Model) NewSession(cfg Config) (*Session, error) {
	ctx, err := mm.m.NewContext()
	if err != nil {
		return nil, fmt.Errorf("new context: %w", err)
	}
	if err := configureContext(ctx, cfg); err != nil {
		return nil, err
	}
	return &Session{ctx: ctx}, nil
}

// Run does one inference pass over the PCM and returns the post-processed
// text plus a confidence score in [0,1] — the mean per-token probability
// reported by Whisper across all segments. Confidence is a weak signal on
// its own (the model can be confidently wrong on fast-mode output), so
// callers combine it with a textual heuristic.
func (s *Session) Run(pcm []float32) (text string, confidence float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ctx.Process(pcm, nil, nil, nil); err != nil {
		return "", 0, fmt.Errorf("process: %w", err)
	}

	var segs []string
	var sumP float64
	var nTok int
	for {
		seg, segErr := s.ctx.NextSegment()
		if segErr != nil {
			break
		}
		if txt := strings.TrimSpace(seg.Text); txt != "" {
			segs = append(segs, txt)
		}
		for _, tok := range seg.Tokens {
			sumP += float64(tok.P)
			nTok++
		}
	}

	conf := 0.0
	if nTok > 0 {
		conf = sumP / float64(nTok)
	}
	return formatSegments(segs), conf, nil
}
