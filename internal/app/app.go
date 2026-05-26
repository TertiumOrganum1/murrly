// Package app contains the state machine wiring all murrly components.
package app

import (
	"context"
	"log"
	"sync/atomic"
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
	// PadSilence is the initial value of the live-toggle. When true,
	// every transcribe call (including the first, not just reprocess
	// retries) gets baselineSilencePadSec of leading and trailing
	// silence wrapped around the captured PCM. Useful when Whisper
	// keeps clipping the first/last word; the menu surfaces a
	// checkbox so the user can flip it at runtime via SetPadSilence.
	PadSilence bool
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
	// padSilence is the live state for the "pad every sample with 1 s
	// silence at both ends" toggle (config.whisper.pad_silence).
	// Read on the App goroutine in finish()/reprocess(); written from
	// the menu callback via SetPadSilence — atomic to avoid mutex
	// scaffolding for a single boolean.
	padSilence atomic.Bool
}

const (
	// pcmSampleRateHz mirrors the recorder's fixed sample rate;
	// duplicated here so the App can compute audio lengths and
	// silence-prefix samples without importing the recorder.
	pcmSampleRateHz = 16000
	// reprocessSilencePadSec — base unit of leading silence each
	// reprocess click contributes. Click N prepends N seconds of
	// silence on top of any baseline pad — enough to land Whisper's
	// 30 s chunk boundary on a different sample of the audio and
	// produce a different decode path even at T=0.
	reprocessSilencePadSec = 1.0
	// baselineSilencePadSec — added to both ends of every clip when
	// the pad_silence option is on, including the very first
	// transcription. Each reprocess click stacks its own
	// reprocessSilencePadSec on top of the baseline at the start;
	// the trailing pad stays at the baseline regardless of reprocess
	// count (the trailer's job is to keep the last word from
	// touching the chunk boundary, not to perturb the search).
	baselineSilencePadSec = 1.0
)

func New(cfg Config) *App {
	if cfg.PasteDelay == 0 {
		cfg.PasteDelay = 80 * time.Millisecond
	}
	a := &App{cfg: cfg, state: StateIdle}
	a.padSilence.Store(cfg.PadSilence)
	return a
}

// SetPadSilence flips the pad-silence behaviour at runtime. The menu
// renderers call this on toggle so subsequent transcriptions immediately
// pick up the new state. The corresponding flag in config.toml is
// persisted separately by main (so the new value survives a restart).
func (a *App) SetPadSilence(on bool) { a.padSilence.Store(on) }

// PadSilenceOn returns the current pad-silence state for menu rendering.
func (a *App) PadSilenceOn() bool { return a.padSilence.Load() }

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

	toTranscribe := pcm
	if a.padSilence.Load() {
		toTranscribe = padPCM(pcm, baselineSilencePadSec, baselineSilencePadSec)
	}
	a.transcribeAndPaste(toTranscribe)
}

// reprocess re-runs the last captured PCM with a silence prefix
// prepended. Click N prepends N * reprocessSilencePadSec seconds of
// leading silence (1 s, 2 s, 3 s, …) — every click lands Whisper's
// 30 s chunk boundary on a different sample of the audio, which is
// what produces a different decode path at T=0. If the pad_silence
// option is on, baselineSilencePadSec is added on top at the start
// and the trailing pad sits at baselineSilencePadSec regardless of
// click count. The counter resets the next time finish() captures
// fresh audio.
func (a *App) reprocess() {
	if len(a.lastPCM) == 0 {
		log.Printf("reprocess: no saved audio to re-run")
		return
	}
	a.reprocessAttempts++
	startPad := reprocessSilencePadSec * float64(a.reprocessAttempts)
	endPad := 0.0
	if a.padSilence.Load() {
		startPad += baselineSilencePadSec
		endPad = baselineSilencePadSec
	}
	padded := padPCM(a.lastPCM, startPad, endPad)
	origSec := float64(len(a.lastPCM)) / float64(pcmSampleRateHz)
	log.Printf("reprocess: attempt #%d, re-running last %.2fs of audio with %.1fs leading, %.1fs trailing silence", a.reprocessAttempts, origSec, startPad, endPad)
	a.transcribeAndPaste(padded)
}

// padPCM returns a new buffer with startSec of zero samples
// prepended and endSec of zero samples appended to pcm. Either
// duration may be 0 — the caller passes 0 for "no pad".
func padPCM(pcm []float32, startSec, endSec float64) []float32 {
	startSamples := int(startSec * pcmSampleRateHz)
	endSamples := int(endSec * pcmSampleRateHz)
	out := make([]float32, startSamples+len(pcm)+endSamples)
	copy(out[startSamples:], pcm)
	return out
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
