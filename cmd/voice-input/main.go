package main

import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tertiumorganum1/voice-input/internal/app"
	"github.com/tertiumorganum1/voice-input/internal/clipboard"
	"github.com/tertiumorganum1/voice-input/internal/config"
	"github.com/tertiumorganum1/voice-input/internal/hotkey"
	"github.com/tertiumorganum1/voice-input/internal/paster"
	"github.com/tertiumorganum1/voice-input/internal/recorder"
	"github.com/tertiumorganum1/voice-input/internal/transcriber"
	"github.com/tertiumorganum1/voice-input/internal/tray"
)

//go:embed assets/icon-idle.png assets/icon-recording.png assets/icon-transcribing.png assets/icon-error.png
var iconFS embed.FS

func main() {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := recorder.InitPortAudio(); err != nil {
		log.Fatalf("portaudio: %v", err)
	}
	defer recorder.TerminatePortAudio()

	tr, err := transcriber.New(transcriber.Config{
		ModelPath:     cfg.Whisper.ModelPath,
		Language:      cfg.Whisper.Language,
		BeamSize:      cfg.Whisper.BeamSize,
		InitialPrompt: cfg.Whisper.InitialPrompt,
	})
	if err != nil {
		log.Fatalf("transcriber: %v", err)
	}
	defer tr.Close()

	cb := clipboard.New()
	cb.RestorePrimary = cfg.Output.RestorePrimary

	icons := map[tray.State][]byte{
		tray.StateIdle:         mustReadIcon("assets/icon-idle.png"),
		tray.StateRecording:    mustReadIcon("assets/icon-recording.png"),
		tray.StateTranscribing: mustReadIcon("assets/icon-transcribing.png"),
		tray.StateError:        mustReadIcon("assets/icon-error.png"),
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := tray.New(icons, cfgPath, tray.Actions{
		OnQuit: cancel,
	})

	a := app.New(app.Config{
		Recorder:    recorder.New(),
		Transcriber: tr,
		Clipboard:   clipAdapter{cb},
		Paster:      paster.New(),
		PasteDelay:  time.Duration(cfg.Output.PasteDelayMs) * time.Millisecond,
		OnState:     func(s app.State) { t.SetState(toTrayState(s)) },
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

func mustReadIcon(path string) []byte {
	b, err := iconFS.ReadFile(path)
	if err != nil {
		log.Fatalf("embed read %s: %v", path, err)
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
