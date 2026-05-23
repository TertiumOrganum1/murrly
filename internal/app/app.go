// Package app contains the state machine wiring all murrly components.
package app

import (
	"context"
	"log"
	"time"
)

type State int

const (
	StateIdle State = iota
	StateRecording
	StateTranscribing
	StateError
)

type Event int

const (
	EventKeyDown Event = iota
	EventKeyUp
)

type Recorder interface {
	Start() error
	Stop() ([]float32, error)
}

type Transcriber interface {
	Transcribe([]float32) (string, error)
}

// Clipboard returns an opaque snapshot from Save that is passed back to
// Restore. The app does not introspect it.
type Clipboard interface {
	Save() (any, error)
	Set(string) error
	Restore(any) error
}

type Paster interface {
	Paste() error
}

type Config struct {
	Recorder     Recorder
	Transcriber  Transcriber
	Clipboard    Clipboard
	Paster       Paster
	OnState      func(State)
	OnTranscript func(string)
	// AdjustText is an optional last-mile hook applied after the
	// transcriber finishes filtering and before the text reaches the
	// clipboard. It exists for context-aware adjustments — e.g. read
	// the focused UI element's surroundings and adapt casing /
	// leading whitespace / terminator. Returning the input unchanged
	// is a valid no-op (and the default when AdjustText is nil).
	AdjustText func(string) string
	PasteDelay time.Duration
}

type App struct {
	cfg   Config
	state State
}

func New(cfg Config) *App {
	if cfg.PasteDelay == 0 {
		cfg.PasteDelay = 80 * time.Millisecond
	}
	return &App{cfg: cfg, state: StateIdle}
}

func (a *App) Run(ctx context.Context, events <-chan Event) {
	a.setState(StateIdle)
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			a.handle(ev)
		}
	}
}

func (a *App) handle(ev Event) {
	switch a.state {
	case StateIdle, StateError:
		if ev == EventKeyDown {
			if err := a.cfg.Recorder.Start(); err != nil {
				log.Printf("recorder.Start: %v", err)
				a.setState(StateError)
				return
			}
			a.setState(StateRecording)
		}
	case StateRecording:
		if ev == EventKeyUp {
			a.finish()
		}
	}
}

func (a *App) finish() {
	pcm, err := a.cfg.Recorder.Stop()
	if err != nil {
		log.Printf("recorder.Stop: %v", err)
		a.setState(StateError)
		return
	}
	if len(pcm) == 0 {
		log.Printf("recording is empty")
		a.setState(StateIdle)
		return
	}
	a.setState(StateTranscribing)
	audioSec := float64(len(pcm)) / 16000.0
	t0 := time.Now()
	text, err := a.cfg.Transcriber.Transcribe(pcm)
	transcribeMs := time.Since(t0).Milliseconds()
	if err != nil {
		log.Printf("transcribe: %v", err)
		a.setState(StateError)
		return
	}
	log.Printf("transcribed (audio=%.2fs, took=%dms, rtf=%.2fx): %q", audioSec, transcribeMs, float64(transcribeMs)/(audioSec*1000), text)
	if text == "" {
		a.setState(StateIdle)
		return
	}
	// History/tray sees the canonical recognised text (before the
	// context-aware tweaks) — those are insertion-point specific and
	// would just clutter the recent-transcripts menu with a leading
	// space etc.
	if a.cfg.OnTranscript != nil {
		a.cfg.OnTranscript(text)
	}
	if a.cfg.AdjustText != nil {
		// AdjustText handles its own logging — it has the AX-status
		// detail (which lookup step succeeded, what preceding char
		// was returned) that we'd be lying about here without context.
		text = a.cfg.AdjustText(text)
	}

	saved, err := a.cfg.Clipboard.Save()
	if err != nil {
		log.Printf("clipboard.Save: %v", err)
		a.setState(StateError)
		return
	}
	if err := a.cfg.Clipboard.Set(text); err != nil {
		log.Printf("clipboard.Set: %v", err)
		a.setState(StateError)
		return
	}
	if err := a.cfg.Paster.Paste(); err != nil {
		log.Printf("paster.Paste: %v", err)
		a.setState(StateError)
		return
	}
	time.Sleep(a.cfg.PasteDelay)
	if err := a.cfg.Clipboard.Restore(saved); err != nil {
		log.Printf("clipboard.Restore: %v", err)
	}
	a.setState(StateIdle)
}

func (a *App) setState(s State) {
	a.state = s
	if a.cfg.OnState != nil {
		a.cfg.OnState(s)
	}
}
