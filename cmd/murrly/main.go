package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/autostart"
	"github.com/tertiumorganum1/murrly/internal/clipboard"
	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/dockmenu"
	"github.com/tertiumorganum1/murrly/internal/hotkey"
	"github.com/tertiumorganum1/murrly/internal/logfile"
	"github.com/tertiumorganum1/murrly/internal/macospermissions"
	"github.com/tertiumorganum1/murrly/internal/menuactions"
	"github.com/tertiumorganum1/murrly/internal/modelinfo"
	"github.com/tertiumorganum1/murrly/internal/multiinfer"
	"github.com/tertiumorganum1/murrly/internal/overlay"
	"github.com/tertiumorganum1/murrly/internal/paster"
	"github.com/tertiumorganum1/murrly/internal/paths"
	"github.com/tertiumorganum1/murrly/internal/picker"
	"github.com/tertiumorganum1/murrly/internal/recorder"
	"github.com/tertiumorganum1/murrly/internal/transcriber"
	"github.com/tertiumorganum1/murrly/internal/transcripthistory"
	"github.com/tertiumorganum1/murrly/internal/tray"
	// internal/uicontext intentionally not wired here. It reads the
	// focused UI element via macOS Accessibility, which works fine in
	// native apps (Notes, TextEdit, Safari URL bar) but reliably
	// returns no-focused for Electron / webview-based targets — and
	// the latter is where Murrly is actually used the most (VS Code
	// chat panels, Slack, Discord, ...). Until we have a workable
	// path through those, the package sits unused; activating it for
	// 30% of cases would only confuse the behaviour ("works here,
	// silently no-ops there"). See conversation 2026-05-23.
)

// iconFS and iconDir are provided by icons_linux.go / icons_darwin.go —
// the tray pack is different per platform (Linux gets the colored cat,
// macOS keeps the menu-bar-style monochrome silhouettes everyone else
// uses up there).

func main() {
	closeLog := setupLogging()
	defer closeLog()

	setupMetalResources()

	cfgPath, err := config.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if !macospermissions.EnsureAccessibility() {
		// First-launch prompt. macOS only shows the system alert once
		// per TCC state — for a real user this is a single dialog they
		// either accept or dismiss. During development the cdhash
		// changes on every rebuild and install-mac.sh resets TCC, so
		// the prompt fires repeatedly; if many invisibly stack up,
		// `killall UserNotificationCenter` clears them.
		log.Printf("accessibility: not granted yet — paste will not work until you enable Murrly in System Settings → Privacy & Security → Accessibility")
	}
	if err := recorder.InitPortAudio(); err != nil {
		log.Fatalf("portaudio: %v", err)
	}
	defer recorder.TerminatePortAudio()

	// Surface the mic permission prompt right at startup by momentarily
	// opening (and immediately closing) a default input stream. Only
	// stream.Start() reliably triggers the macOS TCC dialog — bare
	// AudioUnitInitialize or AVCaptureDevice.requestAccess can both be
	// silent in this app shape, so we go through the same PortAudio
	// path the real record code uses. On Linux this is a harmless
	// noop-ish open/close.
	if err := recorder.Probe(); err != nil {
		log.Printf("mic probe: %v", err)
	}

	trCfg := transcriber.Config{
		ModelPath:     cfg.Whisper.ModelPath,
		Language:      cfg.Whisper.Language,
		BeamSize:      cfg.Whisper.BeamSize,
		BeamAdaptive:  cfg.Whisper.BeamAdaptive,
		InitialPrompt: cfg.Whisper.InitialPrompt,
	}
	// Exactly one inference engine is built, by count:
	//   count == 1 → single in-process Transcriber (with model hot-swap).
	//   count  > 1 → multiinfer.Runner (one model, sequential variants).
	// Building only one avoids loading the model twice. The active
	// engine's model swap / config reload is wired below through the
	// shared menu callbacks.
	var loader *transcriberLoader
	var multiRunner *multiinfer.Runner
	scoreMode := multiinfer.ParseScoreMode(cfg.Whisper.ScoringMode)
	if n := cfg.Whisper.MultiInferenceCount; n > 1 {
		multiRunner, err = multiinfer.New(trCfg, n, scoreMode)
		if err != nil {
			log.Fatalf("multi-inference: %v", err)
		}
		log.Printf("multi-inference: %d sequential variants per recording (model %s, scoring %s)", n, cfg.Whisper.Model, scoreMode)
	} else {
		tr, terr := transcriber.New(trCfg)
		if terr != nil {
			log.Fatalf("transcriber: %v", terr)
		}
		loader = newTranscriberLoader(tr, cfg.Whisper)
	}

	// switchModel / reloadConfig route the model-picker and reload-config
	// menu actions to whichever engine is active.
	switchModel := func(name string) error {
		if multiRunner != nil {
			dir, derr := paths.ModelsDir()
			if derr != nil {
				return derr
			}
			return multiRunner.Reload(filepath.Join(dir, "ggml-"+name+".bin"))
		}
		return loader.Reload(name)
	}
	reloadConfig := func() error {
		if multiRunner != nil {
			newCfg, lerr := config.Load(cfgPath)
			if lerr != nil {
				return lerr
			}
			return multiRunner.ReloadConfig(transcriber.Config{
				ModelPath:     newCfg.Whisper.ModelPath,
				Language:      newCfg.Whisper.Language,
				BeamSize:      newCfg.Whisper.BeamSize,
				BeamAdaptive:  newCfg.Whisper.BeamAdaptive,
				InitialPrompt: newCfg.Whisper.InitialPrompt,
			})
		}
		return loader.ReloadConfig(cfgPath)
	}

	cb := clipboard.New()
	cb.RestorePrimary = cfg.Output.RestorePrimary

	icons := map[tray.State][]byte{
		tray.StateIdle:         mustReadIcon("idle_44.png"),
		tray.StateRecording:    mustReadIcon("recording_44.png"),
		tray.StateTranscribing: mustReadIcon("transcribing_44.png"),
		tray.StateError:        mustReadIcon("error_44.png"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	history := transcripthistory.New(3)

	// Filter the model picker to only models actually downloaded — if
	// the user has just one, the submenu is hidden entirely (nothing to
	// pick). Bootstrap with MODELS=all populates all three.
	presentModelNames, presentModelLabels := presentModels()

	// events drives the App state machine. Declared early so the
	// menu callbacks (OnReprocess in particular) can send into it.
	// The hotkey goroutine below pumps EventKeyDown/Up into the same
	// channel.
	events := make(chan app.Event, 8)

	var t *tray.Tray
	var a *app.App
	actions := &menuactions.Actions{
		OnCopyTranscript: func(index int) {
			text, ok := history.Get(index)
			if !ok {
				return
			}
			if err := cb.Set(text); err != nil {
				log.Printf("copy transcript %d: %v", index, err)
			}
		},
		OnPickModel: func(index int) {
			if index < 0 || index >= len(presentModelNames) {
				return
			}
			name := presentModelNames[index]
			if err := switchModel(name); err != nil {
				log.Printf("model pick: %v", err)
				return
			}
			if err := persistModelChoice(cfgPath, cfg, name); err != nil {
				log.Printf("model pick: persist config: %v", err)
			}
			dockmenu.SetActiveModel(index)
			t.SetActiveModel(index)
		},
		ModelLabels:      presentModelLabels,
		ActiveModelIndex: indexOf(presentModelNames, cfg.Whisper.Model),
		OnReloadConfig: func() {
			if err := reloadConfig(); err != nil {
				log.Printf("reload config: %v", err)
			}
		},
		OnOpenConfig: func() { openConfigFile(cfgPath) },
		OnReprocess: func() {
			// Non-blocking: if the channel is full (very unusual —
			// it's buffered to 8 events) we drop the click rather
			// than stall the menu thread. App.handle ignores
			// EventReprocess unless state is Idle/Error, so
			// click-while-busy is naturally a no-op.
			select {
			case events <- app.EventReprocess:
			default:
				log.Printf("reprocess: event channel full, ignored")
			}
		},
		IsAutostartOn: autostart.Enabled,
		OnToggleAutostart: func() bool {
			if autostart.Enabled() {
				if err := autostart.Disable(); err != nil {
					log.Printf("autostart disable: %v", err)
				}
			} else {
				if err := autostart.Enable(); err != nil {
					log.Printf("autostart enable: %v", err)
				}
			}
			newState := autostart.Enabled()
			// Tray uses the return value to update its own checkmark; the
			// dock menu has its own NSMenuItem state, sync it here.
			dockmenu.SetAutostart(newState)
			return newState
		},
		IsPadSilenceOn: func() bool {
			if a == nil {
				return cfg.Whisper.PadSilence
			}
			return a.PadSilenceOn()
		},
		OnTogglePadSilence: func() bool {
			newState := !a.PadSilenceOn()
			a.SetPadSilence(newState)
			if err := persistPadSilence(cfgPath, cfg, newState); err != nil {
				log.Printf("pad-silence persist: %v", err)
			}
			cfg.Whisper.PadSilence = newState
			return newState
		},
		OnQuit: func() { cancel(); t.Quit() },
	}
	if privacyPanesSupported() {
		actions.OnOpenMicSettings = func() {
			// When the user has not yet answered the prompt, the right
			// UX is to *show the prompt*, not bounce them to Settings
			// where Murrly doesn't even appear in the list yet. Once a
			// decision exists (granted or denied), the prompt won't
			// reappear, and Settings is the only place to flip it —
			// fall through there.
			if macospermissions.MicrophoneAuthStatus() == 0 {
				if err := recorder.Probe(); err != nil {
					log.Printf("mic probe: %v", err)
				}
				return
			}
			openPrivacyPane("Microphone")
		}
		actions.OnOpenAccessibility = func() { openPrivacyPane("Accessibility") }
	}
	// Scoring-mode menu (multi-inference only): switch live between the
	// combined blend, Whisper confidence alone, and the text-shape
	// heuristic alone — and persist the choice. Single-pass mode has no
	// variants to rank, so the group stays hidden there.
	if multiRunner != nil {
		actions.ScoringLabels = scoringModeLabels
		actions.ActiveScoringIndex = scoringIndexOf(scoreMode)
		actions.OnPickScoringMode = func(index int) {
			if index < 0 || index >= len(scoringModeOrder) {
				return
			}
			mode := scoringModeOrder[index]
			multiRunner.SetScoreMode(mode)
			if err := persistScoringMode(cfgPath, cfg, mode.String()); err != nil {
				log.Printf("scoring mode persist: %v", err)
			}
			cfg.Whisper.ScoringMode = mode.String()
			// Keep both menus' checkmarks in sync regardless of which one
			// the pick came from (tray vs Dock).
			dockmenu.SetActiveScoring(index)
			t.SetActiveScoring(index)
			log.Printf("scoring mode -> %s (applies to next recording / Ctrl+F12 batch)", mode)
		}

		// Live multi-inference on/off. When off, F12 does a single pass on
		// the original sample (no variant batch, no Ctrl+F11 picker) and
		// Ctrl+F12 reprocess behaves like the old single-pass retry.
		actions.IsMultiOn = func() bool {
			if a == nil {
				return cfg.Whisper.MultiInference
			}
			return a.MultiInferenceOn()
		}
		actions.OnToggleMulti = func() bool {
			newState := !a.MultiInferenceOn()
			a.SetMultiInference(newState)
			if err := persistMultiInference(cfgPath, cfg, newState); err != nil {
				log.Printf("multi-inference persist: %v", err)
			}
			cfg.Whisper.MultiInference = newState
			dockmenu.SetMulti(newState)
			return newState
		}
	}
	t = tray.New(icons, actions)

	appCfg := app.Config{
		Recorder:    recorder.New(),
		Transcriber: loader,
		Clipboard:   clipAdapter{cb},
		Paster:      paster.New(),
		PasteDelay:  time.Duration(cfg.Output.PasteDelayMs) * time.Millisecond,
		PadSilence:  cfg.Whisper.PadSilence,
		// AdjustText (context-aware insertion-point adaptation) is
		// intentionally not wired — see the uicontext import comment.
		OnState: func(s app.State) {
			t.SetState(toTrayState(s))
			switch s {
			case app.StateRecording:
				overlay.Show(icons[tray.StateRecording], "Listening…")
			case app.StateTranscribing:
				overlay.Show(icons[tray.StateTranscribing], "Transcribing…")
			case app.StateError:
				overlay.Show(icons[tray.StateError], "Error")
			default:
				overlay.Hide()
			}
		},
		OnTranscript: func(text string) {
			history.Add(text)
			snap := history.Snapshot()
			t.SetRecentTranscripts(snap)
			// Mirror the snapshot into the Dock menu so the right-click
			// menu shows the same three previews as the tray.
			latest, _ := first(snap, 0)
			prev, _ := first(snap, 1)
			older, _ := first(snap, 2)
			dockmenu.SetTranscripts(latest, prev, older)
		},
	}
	// Multi-inference takes over the transcribe path when a runner was
	// built (count > 1) AND the live toggle is on. The same adapter also
	// serves the single-pass path (Transcriber → Runner.RunOne), so turning
	// multi-inference off costs nothing — no second model is loaded. The
	// picker backs Ctrl+F11 selection.
	var ma *multiAdapter
	if multiRunner != nil {
		ma = &multiAdapter{r: multiRunner}
		appCfg.MultiTranscriber = ma
		appCfg.Transcriber = ma // single-pass path when the toggle is off
		appCfg.MultiInference = cfg.Whisper.MultiInference
	}

	// Cross-engine: Whisper + Nemotron. Linux-only — the stub returns nil
	// elsewhere, leaving the legacy Whisper-only path. When wired it owns
	// the recording path (F12 → best Whisper, Break → best Nemotron) and
	// needs the picker for Ctrl+F11.
	whisperSingle := appCfg.Transcriber
	var whisperMulti app.MultiTranscriber
	if ma != nil {
		whisperMulti = ma
	}
	if cross := setupNemotron(events, whisperSingle, whisperMulti); cross != nil {
		appCfg.CrossEngine = cross
	}
	if appCfg.CrossEngine != nil || multiRunner != nil {
		appCfg.Picker = pickerAdapter{}
	}

	a = app.New(appCfg)

	hk, err := hotkey.New(cfg.Hotkey.Key)
	if err != nil {
		log.Fatalf("hotkey: %v", err)
	}
	go hk.Start()

	go func() {
		for e := range hk.Events() {
			switch e {
			case hotkey.EventDown:
				events <- app.EventKeyDown
			case hotkey.EventUp:
				events <- app.EventKeyUp
			}
		}
	}()

	// Ctrl+<hotkey> triggers manual reprocess. Separate Listener because
	// X11 routes by exact modifier state — sharing a grab with the
	// push-to-talk key would lose either the bare or the modified
	// variant. Registration failure (e.g. another app owns the chord)
	// is non-fatal: reprocess stays reachable via the tray/dock menu.
	if reprocessHk, err := hotkey.NewWithCtrl(cfg.Hotkey.Key); err != nil {
		log.Printf("reprocess hotkey: %v — menu item remains the only way to trigger it", err)
	} else {
		go reprocessHk.Start()
		go func() {
			for e := range reprocessHk.Events() {
				if e != hotkey.EventDown {
					continue
				}
				select {
				case events <- app.EventReprocess:
				default:
					log.Printf("reprocess: event channel full, hotkey press ignored")
				}
			}
		}()
	}

	// Ctrl+F11 opens the multi-inference picker over the cached variants.
	// A fixed key distinct from push-to-talk (F12) and reprocess
	// (Ctrl+F12); Alt and Ctrl+Alt variants of F12 turned out to be
	// grabbed by the desktop environment. Wired only when
	// multi-inference is active — without a runner there's nothing to
	// pick. Non-fatal registration like the others.
	if appCfg.Picker != nil {
		if pickHk, err := hotkey.NewWithCtrl("F11"); err != nil {
			log.Printf("picker hotkey: %v", err)
		} else {
			go pickHk.Start()
			go func() {
				for e := range pickHk.Events() {
					if e != hotkey.EventDown {
						continue
					}
					select {
					case events <- app.EventPickCandidate:
					default:
					}
				}
			}()
		}
	}

	go a.Run(ctx, events)

	// Dock right-click menu (macOS only) — same Actions as the tray, so
	// "Открыть конфиг" or "Перезагрузить конфиг" do the same thing
	// regardless of which menu the user opens. On Linux dockmenu.Install
	// is a no-op.
	dockmenu.Install(actions)
	// Reflect current state in the menus on startup.
	dockmenu.SetAutostart(autostart.Enabled())
	dockmenu.SetActiveModel(indexOf(presentModelNames, cfg.Whisper.Model))
	if multiRunner != nil {
		dockmenu.SetActiveScoring(scoringIndexOf(scoreMode))
		dockmenu.SetMulti(cfg.Whisper.MultiInference)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
		t.Quit()
	}()

	t.Run() // blocks until systray.Quit() is called
	hk.Stop()
}

// scoringModeOrder fixes the menu row order; scoringModeLabels are the
// matching tray labels (rendered as "Оценка: <label>"). Index i in the
// menu maps to scoringModeOrder[i].
var (
	scoringModeOrder  = []multiinfer.ScoreMode{multiinfer.ScoreCombined, multiinfer.ScoreConfidence, multiinfer.ScoreHeuristic}
	scoringModeLabels = []string{"вместе", "только Whisper", "только эвристика"}
)

// scoringIndexOf returns the menu row for a mode (0 = combined default).
func scoringIndexOf(m multiinfer.ScoreMode) int {
	for i, e := range scoringModeOrder {
		if e == m {
			return i
		}
	}
	return 0
}

// presentModels returns the subset of modelinfo.Available whose model
// file exists on disk under <ModelsDir>/ggml-<name>.bin, plus the
// matching human-readable labels in the same order. Used to hide
// missing models from the picker — a user with just large-v3 doesn't
// see two clickable-but-broken siblings, and a user with one model
// total sees no picker at all (tray.go hides the "Модель" header
// when fewer than 2 labels are passed).
func presentModels() (names []string, labels []string) {
	dir, err := paths.ModelsDir()
	if err != nil {
		return nil, nil
	}
	for _, name := range modelinfo.Available {
		if _, err := os.Stat(filepath.Join(dir, "ggml-"+name+".bin")); err == nil {
			names = append(names, name)
			labels = append(labels, modelinfo.Labels[name])
		}
	}
	return
}

// indexOf returns the position of v in s, or -1 if absent.
func indexOf(s []string, v string) int {
	for i, e := range s {
		if e == v {
			return i
		}
	}
	return -1
}

// first returns snap[i] (or empty + false if out of range).
func first(snap []string, i int) (string, bool) {
	if i < 0 || i >= len(snap) {
		return "", false
	}
	return snap[i], true
}

// openConfigFile opens the config in the user's default text-file handler.
// macOS: `open path` lets LaunchServices pick the registered .toml handler.
// Linux: `xdg-open` does the same via the freedesktop standard.
func openConfigFile(path string) {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", path)
	} else {
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}

func setupLogging() func() {
	path, err := logfile.DefaultPath(logAppName())
	if err != nil {
		log.Printf("log path: %v", err)
		return func() {}
	}
	file, err := logfile.Open(path, 5*1024*1024, 5)
	if err != nil {
		log.Printf("open log %s: %v", path, err)
		return func() {}
	}
	log.SetOutput(file)
	log.Printf("log file: %s", path)
	return func() {
		if err := file.Close(); err != nil {
			log.Printf("close log: %v", err)
		}
	}
}

func toTrayState(s app.State) tray.State {
	switch s {
	case app.StateRecording:
		return tray.StateRecording
	case app.StateTranscribing:
		return tray.StateTranscribing
	case app.StateError:
		return tray.StateError
	default:
		return tray.StateIdle
	}
}

func mustReadIcon(name string) []byte {
	b, err := iconFS.ReadFile(iconDir + "/" + name)
	if err != nil {
		log.Fatalf("embed read %s: %v", name, err)
	}
	return b
}

// clipAdapter bridges *clipboard.Clipboard to app.Clipboard (any-typed Restore).
type clipAdapter struct{ *clipboard.Clipboard }

func (a clipAdapter) Save() (any, error) {
	s, err := a.Clipboard.Save()
	return s, err
}

func (a clipAdapter) Restore(saved any) error {
	s, ok := saved.(clipboard.Saved)
	if !ok {
		return nil
	}
	return a.Clipboard.Restore(s)
}

// multiAdapter bridges *multiinfer.Runner to app.MultiTranscriber,
// translating multiinfer.Candidate into app.Variant.
type multiAdapter struct{ r *multiinfer.Runner }

func (m *multiAdapter) Count() int { return m.r.Count() }

// Transcribe is the single-pass path (app.Transcriber): one inference over
// the audio as-is, used when the multi-inference toggle is off. Backed by
// the same model/session as the variant batch.
func (m *multiAdapter) Transcribe(pcm []float32) (string, error) {
	return m.r.RunOne(pcm)
}

func (m *multiAdapter) Run(pcm []float32, leadOffsetSec float64) []app.Variant {
	cands := m.r.Run(pcm, leadOffsetSec)
	out := make([]app.Variant, len(cands))
	for i, c := range cands {
		out[i] = app.Variant{
			Text:       c.Text,
			Score:      c.Score,
			Confidence: c.Confidence,
			PadLeadSec: c.PadLeadSec,
		}
	}
	return out
}

// pickerAdapter bridges the platform picker to app.Picker, rendering
// each variant as a single truncated preview line. The variant our
// ranking would auto-insert (highest score) is marked with a leading
// star so the user can see which one the scoring chose.
type pickerAdapter struct{}

func (pickerAdapter) Pick(variants []app.Variant) (int, bool) {
	// Cap the window at the 8 most-recent variants: with two engines and
	// reprocess rounds the cache can grow past what fits on screen and stays
	// scannable. The returned index is mapped back to the full slice.
	start := 0
	if len(variants) > 8 {
		start = len(variants) - 8
	}
	shown := variants[start:]
	best := bestVariant(shown)
	opts := make([]string, len(shown))
	for i, v := range shown {
		p := modelGlyph(v.Model) + variantPreview(v.Text)
		if i == best {
			p = "★ " + p
		}
		opts[i] = p
	}
	idx, ok := picker.Pick("", opts)
	if !ok {
		return 0, false
	}
	return start + idx, true
}

// modelGlyph prefixes a per-engine marker so the picker shows which model
// produced each variant. A glyph (not colour — colour marks hover/selection)
// per the cross-engine design.
func modelGlyph(model string) string {
	switch model {
	case app.ModelWhisper:
		return "Ⓦ "
	case app.ModelNemotron:
		return "Ⓝ "
	default:
		return ""
	}
}

// bestVariant returns the index of the highest-scoring variant (the one
// auto-inserted). -1 only for an empty slice.
func bestVariant(vs []app.Variant) int {
	best := -1
	for i := range vs {
		if best < 0 || vs[i].Score > vs[best].Score {
			best = i
		}
	}
	return best
}

// variantPreview collapses runs of whitespace (including segment-break
// newlines from Whisper) into single spaces. The picker word-wraps and
// caps each card at its own maxCardLines with an ellipsis, so we don't
// truncate here — sending the full text lets a long dictation actually
// fill those five lines instead of stopping at two.
func variantPreview(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
