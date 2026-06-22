// Package app contains the state machine wiring all murrly components.
package app

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tertiumorganum1/murrly/internal/ruprofane"
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
	// transcribing). In multi-inference mode it runs a fresh batch of
	// variants (continuing the leading-silence offset) and inserts the
	// best, appending to the cached set.
	EventReprocess
	// EventPickCandidate (Alt+F12) opens the picker over the variants
	// already computed for the last recording — no new inference. The
	// user-chosen variant replaces the inserted text. No-op when there
	// are no cached variants or no picker is wired (non-Linux).
	EventPickCandidate
	// EventKeyDownNemotron / EventKeyUpNemotron mirror EventKeyDown/Up but
	// route the recording through the Nemotron engine (the Break key,
	// Linux-only). Whisper stays on EventKeyDown/Up (F12). No-op when no
	// NemotronTranscriber is wired.
	EventKeyDownNemotron
	EventKeyUpNemotron
	// EventReprocessNemotron is Ctrl+Break: re-run the last PCM through both
	// engines and insert the best Nemotron variant (Ctrl+F12 → best Whisper).
	EventReprocessNemotron
	// EventKeyDownForceMid / EventKeyUpForceMid mirror EventKeyDown/Up but
	// FORCE the mid-sentence insert transform (Shift+F12): the recognised
	// text is decapitalised, gets a single leading space and its terminal
	// punctuation stripped, with no field reading at all. For when the user
	// knows they're slipping text into the middle of a phrase and doesn't
	// want the context heuristics to guess.
	EventKeyDownForceMid
	EventKeyUpForceMid
)

type Recorder interface {
	Start() error
	Stop() ([]float32, error)
}

type Transcriber interface {
	Transcribe([]float32) (string, error)
}

// Variant is one multi-inference candidate surfaced to the app: the
// transcribed text plus the scores used to rank it. The app stays
// agnostic about how variants are produced or scored — that's
// MultiTranscriber's job.
type Variant struct {
	Text       string
	Score      float64
	Confidence float64
	PadLeadSec float64
	// Model tags which engine produced this variant ("whisper" / "nemotron").
	// Empty on the legacy single-engine paths. Drives per-model best
	// selection (F12 → Whisper, Break → Nemotron) and the picker glyph.
	Model string
	// Inserted marks the variant that was actually auto-inserted for this
	// dictation (the pressed engine's best). The picker stars THIS one,
	// instead of a cross-model "highest score" that compares incomparable
	// Whisper/Nemotron scales and lands on the wrong card.
	Inserted bool
}

// Engine model tags carried on Variant.Model.
const (
	ModelWhisper  = "whisper"
	ModelNemotron = "nemotron"
)

// MultiTranscriber runs several inference variants over one sample and
// returns them ranked best-first. leadOffsetSec is added to every
// variant's leading-silence shift so successive reprocess rounds explore
// fresh chunk alignments instead of repeating the first batch. nil in
// Config means single-pass mode (use Transcriber).
type MultiTranscriber interface {
	Run(pcm []float32, leadOffsetSec float64) []Variant
	Count() int
}

// NemotronEngine produces Nemotron-only variants, ranked best-first. Driven
// by the Break key. F12 stays on the Whisper Transcriber/MultiTranscriber
// path so it keeps its original speed — we deliberately do NOT run both
// engines on every dictation (Nemotron's per-call overhead made that ~10×
// slower on short phrases). multi → full variant batch; false → single pass.
// leadOffsetSec feeds reprocess-round diversification; Count is the batch size.
type NemotronEngine interface {
	Run(pcm []float32, leadOffsetSec float64, multi bool) []Variant
	Count() int
}

// Picker shows the cached variants and returns the chosen index, or
// ok=false if the user cancelled or no picker UI is available.
type Picker interface {
	Pick(variants []Variant) (index int, ok bool)
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
	// Nemotron, when non-nil, is the second engine driven by the Break key.
	// F12 keeps using Transcriber / MultiTranscriber (Whisper) at full speed;
	// Break runs Nemotron only. nil → Break ignored (non-Linux / disabled).
	Nemotron     NemotronEngine
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
	// AdjustTextForced is the Shift+F12 variant of AdjustText: it applies
	// the mid-sentence transform unconditionally (decapitalise, leading
	// space, strip terminator) without reading the focused field. Called
	// instead of AdjustText for recordings started by EventKeyDownForceMid.
	// nil → falls back to AdjustText.
	AdjustTextForced func(string) string
	// Notify shows a transient desktop notification (title, body). Used for
	// the silent-mic case — a NON-error condition that must not raise the red
	// error icon. nil → no notification.
	Notify     func(title, body string)
	PasteDelay time.Duration
	// MultiTranscriber, when non-nil, switches F12/Ctrl+F12 to
	// multi-inference: run N variants, score, insert the best, cache the
	// rest. nil → single-pass via Transcriber (current behavior).
	MultiTranscriber MultiTranscriber
	// Picker renders the cached variants for Alt+F12. nil → Alt+F12 is
	// a no-op (e.g. non-Linux, or zenity unavailable).
	Picker Picker
	// MultiInference is the initial state of the live multi-inference
	// toggle. When MultiTranscriber is wired but this is false, F12 runs a
	// single pass via Transcriber (no variant batch, no picker); when true
	// it runs the full batch. Flipped at runtime via SetMultiInference.
	// Ignored when MultiTranscriber is nil (nothing to toggle).
	MultiInference bool
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
	// lastVariants holds every multi-inference variant computed for the
	// current recording (across the initial F12 batch and any Ctrl+F12
	// reprocess batches), ranked within each batch. Alt+F12's picker
	// chooses from this set. Reset when finish() captures fresh audio.
	lastVariants []Variant
	// multiRound counts Ctrl+F12 reprocess batches for the current
	// recording (0 = the initial F12 batch). Drives the leading-silence
	// offset so each batch explores new chunk alignments.
	multiRound int
	// padSilence is the live state for the "pad every sample with 1 s
	// silence at both ends" toggle (config.whisper.pad_silence).
	// Read on the App goroutine in finish()/reprocess(); written from
	// the menu callback via SetPadSilence — atomic to avoid mutex
	// scaffolding for a single boolean.
	padSilence atomic.Bool
	// multiOn is the live on/off for multi-inference (config.whisper.
	// multi_inference). Read on the App goroutine in finish()/reprocess();
	// written from the menu toggle via SetMultiInference — atomic, like
	// padSilence, to avoid mutex scaffolding for a single boolean.
	multiOn atomic.Bool
	// preferNemotron records which engine's best to INSERT for the current
	// recording/reprocess: true when triggered by the Break family
	// (EventKeyDownNemotron / EventReprocessNemotron), false for F12 /
	// Ctrl+F12. Both keys still run BOTH engines via CrossEngine — this only
	// picks which model's top variant lands in the window. Touched only on
	// the App goroutine — no sync needed.
	preferNemotron bool
	// forceMid is true for the current recording/insert when it was started
	// by Shift+F12 (EventKeyDownForceMid): insertText then uses
	// AdjustTextForced (unconditional mid-sentence transform) instead of the
	// context-reading AdjustText. Set on the force keydown, cleared on every
	// other keydown / reprocess / picker entry. App goroutine only.
	forceMid bool
	// varMu guards lastVariants and varGen. lastVariants is appended both
	// from the App goroutine (the engine whose result is inserted) AND from a
	// background Nemotron goroutine on the F12 path, so it needs a lock.
	// varGen bumps on every fresh recording; a background append checks it so
	// a slow Nemotron run from a previous utterance can't leak into the next
	// one's picker set.
	varMu  sync.Mutex
	varGen uint64
}

// resetVariants clears the picker cache and bumps the generation. Called when
// finish() captures fresh audio, so any in-flight background append is dropped.
func (a *App) resetVariants() {
	a.varMu.Lock()
	a.lastVariants = nil
	a.varGen++
	a.varMu.Unlock()
}

// appendVariants appends on the App goroutine and returns the pre-append count
// (for log numbering).
func (a *App) appendVariants(vs []Variant) int {
	a.varMu.Lock()
	base := len(a.lastVariants)
	a.lastVariants = append(a.lastVariants, vs...)
	a.varMu.Unlock()
	return base
}

// appendVariantsGen appends from a background goroutine, but only if the
// generation still matches (i.e. no newer recording has started).
func (a *App) appendVariantsGen(gen uint64, vs []Variant) bool {
	a.varMu.Lock()
	defer a.varMu.Unlock()
	if gen != a.varGen {
		return false
	}
	a.lastVariants = append(a.lastVariants, vs...)
	return true
}

// markInserted flags lastVariants[idx] as the auto-inserted variant and
// clears the flag on the rest, so the picker stars exactly what landed in
// the window (not a cross-model "highest score").
func (a *App) markInserted(idx int) {
	a.varMu.Lock()
	for i := range a.lastVariants {
		a.lastVariants[i].Inserted = i == idx
	}
	a.varMu.Unlock()
}

func (a *App) curGen() uint64 {
	a.varMu.Lock()
	defer a.varMu.Unlock()
	return a.varGen
}

// snapshotVariants returns a copy for the picker to render without holding
// the lock during the (blocking) UI call.
func (a *App) snapshotVariants() []Variant {
	a.varMu.Lock()
	defer a.varMu.Unlock()
	return append([]Variant(nil), a.lastVariants...)
}

// kickNemotronBackground runs Nemotron off the critical path (F12 inserts
// Whisper immediately) and folds its variants into the picker cache when
// done. Nemotron is a separate process, so this goroutine shares no mutable
// Go state with the engine — only lastVariants, which is mutex-guarded.
func (a *App) kickNemotronBackground(pcm []float32, leadOffsetSec float64) {
	gen := a.curGen()
	multi := a.multiOn.Load()
	go func() {
		vs := a.cfg.Nemotron.Run(pcm, leadOffsetSec, multi)
		if len(vs) == 0 {
			return
		}
		if a.appendVariantsGen(gen, vs) {
			log.Printf("nemotron (background): +%d variants in picker", len(vs))
		}
	}()
}

// kickWhisperBackground mirrors kickNemotronBackground for the Break path:
// Break inserts the Nemotron result immediately, and Whisper runs in the
// BACKGROUND so its variants still surface in the Ctrl+F11 picker. Multi mode
// → the full variant batch; single pass → one variant. Appended only if the
// generation still matches (no newer recording/reprocess has started).
func (a *App) kickWhisperBackground(pcm []float32, leadOffsetSec float64) {
	gen := a.curGen()
	multi := a.multiActive()
	go func() {
		var vs []Variant
		switch {
		case multi:
			vs = a.cfg.MultiTranscriber.Run(pcm, leadOffsetSec)
		case a.cfg.Transcriber != nil:
			if text, err := a.cfg.Transcriber.Transcribe(pcm); err == nil && text != "" {
				vs = []Variant{{Text: text, Model: ModelWhisper}}
			}
		}
		if len(vs) == 0 {
			return
		}
		if a.appendVariantsGen(gen, vs) {
			log.Printf("whisper (background): +%d variants in picker", len(vs))
		}
	}()
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
	// baselineSilencePadStartSec / baselineSilencePadEndSec —
	// asymmetric pads added when the pad_silence option is on. The
	// leading pad anchors the chunk boundary before the first word;
	// the trailing pad is slightly longer because Whisper has a
	// stronger tendency to clip the last token than the first.
	// Each reprocess click stacks its own reprocessSilencePadSec on
	// top of the leading baseline; the trailing pad stays put
	// regardless of reprocess count.
	baselineSilencePadStartSec = 1.0
	baselineSilencePadEndSec   = 1.25
	// multiReprocessStepSec mirrors multiinfer's per-variant leading
	// step. Each Ctrl+F12 reprocess batch offsets its variants by
	// round * count * step so batches don't overlap the same chunk
	// alignments. Kept in sync with multiinfer.padStepSec by value.
	multiReprocessStepSec = 1.5
)

func New(cfg Config) *App {
	if cfg.PasteDelay == 0 {
		cfg.PasteDelay = 80 * time.Millisecond
	}
	a := &App{cfg: cfg, state: StateIdle}
	a.padSilence.Store(cfg.PadSilence)
	a.multiOn.Store(cfg.MultiInference)
	return a
}

// SetPadSilence flips the pad-silence behaviour at runtime. The menu
// renderers call this on toggle so subsequent transcriptions immediately
// pick up the new state. The corresponding flag in config.toml is
// persisted separately by main (so the new value survives a restart).
func (a *App) SetPadSilence(on bool) { a.padSilence.Store(on) }

// PadSilenceOn returns the current pad-silence state for menu rendering.
func (a *App) PadSilenceOn() bool { return a.padSilence.Load() }

// SetMultiInference flips multi-inference at runtime. The menu toggle calls
// this so the next F12 immediately picks up the new mode; main persists the
// flag to config.toml separately so it survives a restart.
func (a *App) SetMultiInference(on bool) { a.multiOn.Store(on) }

// MultiInferenceOn reports the current multi-inference state for menu
// rendering and for the finish()/reprocess() branch.
func (a *App) MultiInferenceOn() bool { return a.multiOn.Load() }

// multiActive is true only when a multi-inference engine is wired AND the
// live toggle is on — the single condition both F12 (finish) and the manual
// reprocess use to choose the variant-batch path over a single pass.
func (a *App) multiActive() bool {
	return a.cfg.MultiTranscriber != nil && a.multiOn.Load()
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
			a.preferNemotron = false
			a.forceMid = false
			if err := a.cfg.Recorder.Start(); err != nil {
				log.Printf("recorder.Start: %v", err)
				a.setState(StateError)
				return
			}
			a.setState(StateRecording)
		case EventKeyDownForceMid:
			// Shift+F12: same Whisper recording path as F12, but the insert
			// is forced through the mid-sentence transform.
			a.preferNemotron = false
			a.forceMid = true
			if err := a.cfg.Recorder.Start(); err != nil {
				log.Printf("recorder.Start: %v", err)
				a.setState(StateError)
				return
			}
			a.setState(StateRecording)
		case EventKeyDownNemotron:
			if a.cfg.Nemotron == nil {
				log.Printf("nemotron: engine not wired, Break ignored")
				return
			}
			if err := a.cfg.Recorder.Start(); err != nil {
				log.Printf("recorder.Start: %v", err)
				a.setState(StateError)
				return
			}
			a.preferNemotron = true
			a.forceMid = false
			a.setState(StateRecording)
		case EventReprocess:
			a.preferNemotron = false
			a.forceMid = false
			a.reprocess()
		case EventReprocessNemotron:
			a.preferNemotron = true
			a.forceMid = false
			a.reprocess()
		case EventPickCandidate:
			a.forceMid = false
			a.pickCandidate()
		}
	case StateRecording:
		if ev == EventKeyUp || ev == EventKeyUpNemotron || ev == EventKeyUpForceMid {
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
	// Capture & clear the engine preference up front so it can't leak into
	// the next recording even if we early-return below (e.g. empty PCM).
	pref := a.preferNemotron
	a.preferNemotron = false
	// forceMid is read by insertText (called synchronously below); reset it on
	// return so it never survives this recording. Without this, an emoji-key
	// TAP (sets forceMid=true, then finishes empty with no insert) would leave
	// the flag set and make the NEXT F12 wrongly force a mid-sentence insert.
	defer func() { a.forceMid = false }()

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
	// Silent recording = mic muted / not selected / hardware gain at zero —
	// NOT an error. Tell the user with a notification and stay idle (no red
	// error icon, which is reserved for real failures); skip the engines and
	// don't save it as lastPCM so reprocess won't re-run silence.
	if p := peak(pcm); p < silenceThreshold {
		dur := float64(len(pcm)) / float64(pcmSampleRateHz)
		log.Printf("recording: %.2fs but silent (peak %.4f) — mic off / not selected / volume at zero", dur, p)
		if a.cfg.Notify != nil {
			a.cfg.Notify("Murrly: тишина", "Микрофон молчит. Выберите другой микрофон или проверьте громкость/выключатель.")
		}
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
	a.multiRound = 0
	a.resetVariants()

	// Break inserts a Nemotron result (and waits for it). F12 inserts the
	// Whisper result immediately and lets Nemotron run in the BACKGROUND —
	// its variants surface in the Ctrl+F11 picker when ready, never blocking
	// the fast Whisper insert.
	if pref && a.cfg.Nemotron != nil {
		a.runNemotron(pcm, 0, "recording")
		a.kickWhisperBackground(pcm, 0) // mirror of F12: the other engine fills the picker in the background
		return
	}

	if a.multiActive() {
		a.runMulti(pcm, 0, "recording")
	} else {
		toTranscribe := pcm
		if a.padSilence.Load() {
			toTranscribe = padPCM(pcm, baselineSilencePadStartSec, baselineSilencePadEndSec)
		}
		a.transcribeAndPaste(toTranscribe)
	}
	if a.cfg.Nemotron != nil {
		a.kickNemotronBackground(pcm, 0)
	}
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
	pref := a.preferNemotron
	a.preferNemotron = false
	if len(a.lastPCM) == 0 {
		log.Printf("reprocess: no saved audio to re-run")
		return
	}

	// Fresh batch ("свежие 8"): clear the cache so Ctrl+F11 shows exactly this
	// round's variants. The generation bump also rejects any background kick
	// still running from the previous round.
	a.resetVariants()
	a.multiRound++

	// Ctrl+Break: insert the Nemotron best, run Whisper in the background.
	if pref && a.cfg.Nemotron != nil {
		offset := float64(a.multiRound*a.cfg.Nemotron.Count()) * multiReprocessStepSec
		a.runNemotron(a.lastPCM, offset, fmt.Sprintf("reprocess #%d", a.multiRound))
		a.kickWhisperBackground(a.lastPCM, offset)
		return
	}

	// Ctrl+F12: insert the Whisper best, run Nemotron in the background. Both
	// engines get the same advancing leading-silence offset so the new batch
	// lands on fresh chunk alignments instead of repeating the previous one.
	if a.multiActive() {
		offset := float64(a.multiRound*a.cfg.MultiTranscriber.Count()) * multiReprocessStepSec
		a.runMulti(a.lastPCM, offset, fmt.Sprintf("reprocess #%d", a.multiRound))
		if a.cfg.Nemotron != nil {
			a.kickNemotronBackground(a.lastPCM, offset)
		}
		return
	}

	// Single-pass Whisper (multi off): stack leading silence per attempt.
	a.reprocessAttempts++
	startPad := reprocessSilencePadSec * float64(a.reprocessAttempts)
	endPad := 0.0
	if a.padSilence.Load() {
		startPad += baselineSilencePadStartSec
		endPad = baselineSilencePadEndSec
	}
	padded := padPCM(a.lastPCM, startPad, endPad)
	origSec := float64(len(a.lastPCM)) / float64(pcmSampleRateHz)
	log.Printf("reprocess: attempt #%d, re-running last %.2fs of audio with %.1fs leading, %.1fs trailing silence", a.reprocessAttempts, origSec, startPad, endPad)
	a.transcribeAndPaste(padded)
	if a.cfg.Nemotron != nil {
		a.kickNemotronBackground(a.lastPCM, startPad)
	}
}

// silenceThreshold is the peak |sample| (portaudio float32 is in [-1,1])
// below which a whole recording counts as silent — a muted / unselected mic
// or a hardware gain knob at zero, not a failure. A dead mic peaks ~0.003;
// real speech peaks well above 0.05.
const silenceThreshold = 0.01

// peak returns the largest absolute sample in pcm.
func peak(pcm []float32) float32 {
	var m float32
	for _, s := range pcm {
		if s < 0 {
			s = -s
		}
		if s > m {
			m = s
		}
	}
	return m
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
	a.transcribeAndPasteWith(a.cfg.Transcriber, pcm)
}

// transcribeAndPasteWith runs the given transcriber over the PCM and
// inserts the result. transcribeAndPaste uses the default (Whisper)
// engine; the Break path passes NemotronTranscriber.
func (a *App) transcribeAndPasteWith(tr Transcriber, pcm []float32) {
	a.setState(StateTranscribing)
	audioSec := float64(len(pcm)) / float64(pcmSampleRateHz)
	t0 := time.Now()
	text, err := tr.Transcribe(pcm)
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
	if err := a.insertText(text); err != nil {
		log.Printf("insert: %v", err)
		a.setState(StateError)
		return
	}
	a.setState(StateIdle)
}

// runMulti drives the multi-inference path: fan out N variants, log the
// whole batch (label is "recording" or "reprocess #N"), insert the
// best, and append the batch to lastVariants for the Alt+F12 picker.
func (a *App) runMulti(pcm []float32, leadOffsetSec float64, label string) {
	a.setState(StateTranscribing)
	audioSec := float64(len(pcm)) / float64(pcmSampleRateHz)
	t0 := time.Now()
	results := a.cfg.MultiTranscriber.Run(pcm, leadOffsetSec)
	took := time.Since(t0)

	if len(results) == 0 {
		log.Printf("%s: %.2fs — all variants empty/failed", label, audioSec)
		a.setState(StateError)
		return
	}

	base := a.appendVariants(results) // continuing variant numbering across batches
	log.Printf("%s: %.2fs (%d variants, took=%v)", label, audioSec, len(results), took.Round(time.Millisecond))
	for i, v := range results {
		log.Printf("  variant %d (pad=%.2fs conf=%.2f score=%.2f): %q", base+i+1, v.PadLeadSec, v.Confidence, v.Score, v.Text)
	}

	best := results[0]
	log.Printf("  -> selected variant %d (best score %.2f), inserted", base+1, best.Score)
	if err := a.insertText(best.Text); err != nil {
		log.Printf("insert: %v", err)
		a.setState(StateError)
		return
	}
	a.markInserted(base) // star the Whisper best in the picker
	a.setState(StateIdle)
}

// runNemotron drives the Break path: Nemotron-only variants (full batch when
// multi is on, else a single pass), ranked best-first by the engine. Caches
// them for the Ctrl+F11 picker and inserts the best. leadOffsetSec feeds the
// reprocess-round diversification; label is "recording" or "reprocess #N".
func (a *App) runNemotron(pcm []float32, leadOffsetSec float64, label string) {
	a.setState(StateTranscribing)
	audioSec := float64(len(pcm)) / float64(pcmSampleRateHz)
	t0 := time.Now()
	results := a.cfg.Nemotron.Run(pcm, leadOffsetSec, a.multiOn.Load())
	took := time.Since(t0)

	if len(results) == 0 {
		log.Printf("%s [nemotron]: %.2fs — no variants (sidecar down?)", label, audioSec)
		a.setState(StateError)
		return
	}

	base := a.appendVariants(results)
	log.Printf("%s [nemotron]: %.2fs (%d variants, took=%v)", label, audioSec, len(results), took.Round(time.Millisecond))
	for i, v := range results {
		log.Printf("  variant %d (score=%.2f): %q", base+i+1, v.Score, v.Text)
	}

	if err := a.insertText(results[0].Text); err != nil {
		log.Printf("insert: %v", err)
		a.setState(StateError)
		return
	}
	a.markInserted(base) // star the Nemotron best in the picker
	a.setState(StateIdle)
}

// pickCandidate (Alt+F12) opens the picker over the cached variants and
// inserts the chosen one. No new inference.
func (a *App) pickCandidate() {
	if a.cfg.Picker == nil {
		return
	}
	// Snapshot under the lock — the background Nemotron goroutine may be
	// appending concurrently.
	vars := a.snapshotVariants()
	if len(vars) == 0 {
		return
	}
	idx, ok := a.cfg.Picker.Pick(vars)
	if !ok || idx < 0 || idx >= len(vars) {
		return
	}
	log.Printf("picker: user chose variant %d (score %.2f)", idx+1, vars[idx].Score)
	if err := a.insertText(vars[idx].Text); err != nil {
		log.Printf("insert: %v", err)
	}
}

// insertText runs the shared insertion path: OnTranscript notification,
// optional AdjustText hook, then the clipboard save / set / paste /
// restore dance. Returns an error on clipboard/paste failure so the
// caller can decide the resulting state; it does not touch state itself.
// A failed Restore is logged but not fatal (the paste already landed).
func (a *App) insertText(text string) error {
	if text == "" {
		return nil
	}
	if a.cfg.OnTranscript != nil {
		a.cfg.OnTranscript(text)
	}
	if a.forceMid && a.cfg.AdjustTextForced != nil {
		text = a.cfg.AdjustTextForced(text)
	} else if a.cfg.AdjustText != nil {
		text = a.cfg.AdjustText(text)
	}
	// Censor обсценную лексику at the very last step before it lands in the
	// clipboard. The stored/scored text (and OnTranscript above) keeps the
	// uncensored original; this is a view-only transform, no-op when the
	// tray toggle is off, so a false positive never destroys the phrase.
	text = ruprofane.Filter(text)

	saved, err := a.cfg.Clipboard.Save()
	if err != nil {
		return fmt.Errorf("clipboard.Save: %w", err)
	}
	if err := a.cfg.Clipboard.Set(text); err != nil {
		return fmt.Errorf("clipboard.Set: %w", err)
	}
	if err := a.cfg.Paster.Paste(); err != nil {
		return fmt.Errorf("paster.Paste: %w", err)
	}
	time.Sleep(a.cfg.PasteDelay)
	if err := a.cfg.Clipboard.Restore(saved); err != nil {
		log.Printf("clipboard.Restore: %v", err)
	}
	return nil
}

func (a *App) setState(s State) {
	a.state = s
	if a.cfg.OnState != nil {
		a.cfg.OnState(s)
	}
}
