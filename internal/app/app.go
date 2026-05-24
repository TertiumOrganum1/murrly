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
	// EventReprocess re-runs the last recorded PCM through the
	// transcriber with a small silence prefix that perturbs Whisper's
	// chunk-boundary alignment and (often) produces a different
	// decode result. Used as a manual "try again" when the user
	// notices a bad transcription. Ignored if no audio has been
	// recorded yet or if the app is currently busy (recording /
	// transcribing).
	EventReprocess
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
	// lastPCM is the most recently captured audio buffer, kept so
	// that EventReprocess can re-run it through Whisper without
	// asking the user to dictate again.
	lastPCM []float32
	// reprocessAttempts counts how many times EventReprocess has
	// fired for the current lastPCM. Reset to 0 every time finish()
	// captures fresh audio. Each click uses (n+1)*reprocessSilencePad
	// of leading silence, so repeated clicks keep perturbing the
	// chunk-boundary alignment differently and (hopefully) land on
	// different decode paths.
	reprocessAttempts int
}

const (
	// pcmSampleRateHz mirrors the recorder's fixed sample rate;
	// duplicated here so the App can compute audio lengths and
	// silence-prefix samples without importing the recorder.
	pcmSampleRateHz = 16000
	// reprocessSilencePadSec — how much silence to prepend before
	// re-running the saved PCM. Shifts Whisper's 30 s chunk
	// boundary by half a second, which is enough to land the
	// decoder on a different search path even at T=0.
	reprocessSilencePadSec = 0.5
)

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
		switch ev {
		case EventKeyDown:
			if err := a.cfg.Recorder.Start(); err != nil {
				log.Printf("recorder.Start: %v", err)
				a.setState(StateError)
				return
			}
			a.setState(StateRecording)
		case EventReprocess:
			a.reprocess()
		}
	case StateRecording:
		if ev == EventKeyUp {
			a.finish()
		}
		// EventReprocess while recording is intentionally ignored
		// — the user is in the middle of capturing new audio.
	case StateTranscribing:
		// All events ignored while a transcription is in flight;
		// the user can re-click reprocess after the overlay clears.
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
	// Save the freshly-captured PCM so EventReprocess can re-run it
	// later without re-recording. We replace any previous one — only
	// the most recent utterance is reachable through "Перепроцессить".
	// Reset the reprocess counter — new audio starts a fresh
	// perturbation sequence.
	a.lastPCM = pcm
	a.reprocessAttempts = 0
	a.transcribeAndPaste(pcm)
}

// reprocess re-runs the last captured PCM with a silence prefix
// prepended. Each successive click on "Перепроцессить" grows the
// prefix by reprocessSilencePadSec, so click 1 adds 0.5 s, click 2
// adds 1.0 s, etc. — every click lands Whisper's 30 s chunk
// boundary on a different sample of the audio, which is what
// produces a different decode path at T=0. The counter resets the
// next time finish() captures fresh audio.
func (a *App) reprocess() {
	if len(a.lastPCM) == 0 {
		log.Printf("reprocess: no saved audio to re-run")
		return
	}
	a.reprocessAttempts++
	padSec := reprocessSilencePadSec * float64(a.reprocessAttempts)
	silentSamples := int(padSec * pcmSampleRateHz)
	padded := make([]float32, silentSamples+len(a.lastPCM))
	copy(padded[silentSamples:], a.lastPCM)
	origSec := float64(len(a.lastPCM)) / float64(pcmSampleRateHz)
	log.Printf("reprocess: attempt #%d, re-running last %.2fs of audio with %.1fs leading silence", a.reprocessAttempts, origSec, padSec)
	a.transcribeAndPaste(padded)
}

// transcribeAndPaste is the shared body of both finish() (F12 path)
// and reprocess() (menu path): transcribe the PCM, optionally adjust
// for the insertion point, save + set + paste the clipboard, then
// restore. Caller owns the choice of whether to update lastPCM (only
// the F12 path does — reprocess shouldn't overwrite the saved
// original with a padded version).
func (a *App) transcribeAndPaste(pcm []float32) {
	a.setState(StateTranscribing)
	audioSec := float64(len(pcm)) / float64(pcmSampleRateHz)
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
	if a.cfg.OnTranscript != nil {
		a.cfg.OnTranscript(text)
	}
	if a.cfg.AdjustText != nil {
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
