// Package app contains the state machine wiring all voice-input components.
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
	Recorder    Recorder
	Transcriber Transcriber
	Clipboard   Clipboard
	Paster      Paster
	OnState     func(State)
	PasteDelay  time.Duration
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
	a.setState(StateTranscribing)
	text, err := a.cfg.Transcriber.Transcribe(pcm)
	if err != nil {
		log.Printf("transcribe: %v", err)
		a.setState(StateError)
		return
	}
	log.Printf("transcribed: %q", text)
	if text == "" {
		a.setState(StateIdle)
		return
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
