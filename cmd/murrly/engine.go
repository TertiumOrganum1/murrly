package main

import (
	"sync"

	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/parakeet"
)

// engineSwitch is the single place that knows which recogniser a dictation goes
// to. Everything downstream — post-processing, the profanity filter, history,
// the insertion routes, the tray — sees one app.Transcriber and cannot tell
// which engine produced the phrase, which is the point: the model picker swaps
// the recogniser and nothing else in the pipeline changes.
//
// whisper is whichever Whisper engine was built at startup (the hot-swappable
// loader, or the variant runner's adapter) and it stays live underneath:
// picking Parakeet does not tear it down, it only stops routing to it, so
// coming back is a menu click rather than a model load.
type engineSwitch struct {
	mu       sync.RWMutex
	whisper  app.Transcriber
	multi    app.MultiTranscriber // nil when no variant runner was built
	parakeet *parakeet.Recognizer // non-nil while Parakeet is the chosen model
}

// Transcribe implements app.Transcriber.
func (e *engineSwitch) Transcribe(pcm []float32) (string, error) {
	e.mu.RLock()
	p, w := e.parakeet, e.whisper
	e.mu.RUnlock()
	if p != nil {
		return p.Transcribe(pcm)
	}
	return w.Transcribe(pcm)
}

// Run implements app.MultiTranscriber. Parakeet has no variant batch — the
// batch is Whisper's trick of re-decoding the same audio at different chunk
// alignments, and a transducer decoded greedily gives the same answer every
// time — so it answers with the single result it has. The picker then shows one
// card instead of an empty one, which is the honest rendering of "this engine
// has one opinion".
func (e *engineSwitch) Run(pcm []float32, leadOffsetSec float64) []app.Variant {
	e.mu.RLock()
	p, m := e.parakeet, e.multi
	e.mu.RUnlock()
	if p == nil {
		if m == nil {
			return nil
		}
		return m.Run(pcm, leadOffsetSec)
	}
	text, err := p.Transcribe(pcm)
	if err != nil || text == "" {
		return nil
	}
	return []app.Variant{{
		Text:  text,
		Score: 1,
		Model: app.ModelParakeet,
	}}
}

// Count implements app.MultiTranscriber.
func (e *engineSwitch) Count() int {
	e.mu.RLock()
	p, m := e.parakeet, e.multi
	e.mu.RUnlock()
	if p != nil {
		return 1
	}
	if m == nil {
		return 1
	}
	return m.Count()
}

// UseParakeet routes subsequent dictations to p (nil routes them back to
// Whisper) and returns the recogniser that was in place, for the caller to
// close once nothing can still be running through it.
func (e *engineSwitch) UseParakeet(p *parakeet.Recognizer) *parakeet.Recognizer {
	e.mu.Lock()
	defer e.mu.Unlock()
	old := e.parakeet
	e.parakeet = p
	return old
}

// OnParakeet reports which engine the next dictation will go to.
func (e *engineSwitch) OnParakeet() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.parakeet != nil
}
