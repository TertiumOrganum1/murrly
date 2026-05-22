package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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
	"github.com/tertiumorganum1/murrly/internal/overlay"
	"github.com/tertiumorganum1/murrly/internal/paster"
	"github.com/tertiumorganum1/murrly/internal/paths"
	"github.com/tertiumorganum1/murrly/internal/recorder"
	"github.com/tertiumorganum1/murrly/internal/transcriber"
	"github.com/tertiumorganum1/murrly/internal/transcripthistory"
	"github.com/tertiumorganum1/murrly/internal/tray"
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

	tr, err := transcriber.New(transcriber.Config{
		ModelPath:     cfg.Whisper.ModelPath,
		Language:      cfg.Whisper.Language,
		BeamSize:      cfg.Whisper.BeamSize,
		Adaptive:      cfg.Whisper.Adaptive,
		InitialPrompt: cfg.Whisper.InitialPrompt,
	})
	if err != nil {
		log.Fatalf("transcriber: %v", err)
	}
	loader := newTranscriberLoader(tr, cfg.Whisper)
	// loader.tr is closed (via Reload's old.Close()) on every model swap;
	// the final one shuts down at program exit when the process dies.

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

	var t *tray.Tray
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
			if err := loader.Reload(name); err != nil {
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
			if err := loader.ReloadConfig(cfgPath); err != nil {
				log.Printf("reload config: %v", err)
			}
		},
		OnOpenConfig: func() { openConfigFile(cfgPath) },
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
	t = tray.New(icons, actions)

	a := app.New(app.Config{
		Recorder:    recorder.New(),
		Transcriber: loader,
		Clipboard:   clipAdapter{cb},
		Paster:      paster.New(),
		PasteDelay:  time.Duration(cfg.Output.PasteDelayMs) * time.Millisecond,
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
	})

	hk, err := hotkey.New(cfg.Hotkey.Key)
	if err != nil {
		log.Fatalf("hotkey: %v", err)
	}
	go hk.Start()

	events := make(chan app.Event, 8)
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

	go a.Run(ctx, events)

	// Dock right-click menu (macOS only) — same Actions as the tray, so
	// "Открыть конфиг" or "Перезагрузить конфиг" do the same thing
	// regardless of which menu the user opens. On Linux dockmenu.Install
	// is a no-op.
	dockmenu.Install(actions)
	// Reflect current state in the menus on startup.
	dockmenu.SetAutostart(autostart.Enabled())
	dockmenu.SetActiveModel(indexOf(presentModelNames, cfg.Whisper.Model))

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
