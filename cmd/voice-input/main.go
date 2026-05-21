package main

import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/clipboard"
	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/hotkey"
	"github.com/tertiumorganum1/murrly/internal/logfile"
	"github.com/tertiumorganum1/murrly/internal/paster"
	"github.com/tertiumorganum1/murrly/internal/recorder"
	"github.com/tertiumorganum1/murrly/internal/transcriber"
	"github.com/tertiumorganum1/murrly/internal/transcripthistory"
	"github.com/tertiumorganum1/murrly/internal/tray"
)

//go:embed assets/icon-idle.png assets/icon-recording.png assets/icon-transcribing.png assets/icon-error.png
var iconFS embed.FS

func main() {
	closeLog := setupLogging()
	defer closeLog()

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
	history := transcripthistory.New(3)

	var t *tray.Tray
	t = tray.New(icons, cfgPath, tray.Actions{
		OnCopyTranscript: func(index int) {
			text, ok := history.Get(index)
			if !ok {
				return
			}
			if err := cb.Set(text); err != nil {
				log.Printf("copy transcript %d: %v", index, err)
			}
		},
		OnQuit: cancel,
	})

	a := app.New(app.Config{
		Recorder:    recorder.New(),
		Transcriber: tr,
		Clipboard:   clipAdapter{cb},
		Paster:      paster.New(),
		PasteDelay:  time.Duration(cfg.Output.PasteDelayMs) * time.Millisecond,
		OnState:     func(s app.State) { t.SetState(toTrayState(s)) },
		OnTranscript: func(text string) {
			history.Add(text)
			t.SetRecentTranscripts(history.Snapshot())
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

func setupLogging() func() {
	path, err := logfile.DefaultPath("voice-input")
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
