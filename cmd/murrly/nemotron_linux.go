//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/hotkey"
	"github.com/tertiumorganum1/murrly/internal/nemotron"
)

// engineManager implements app.CrossEngine: it runs Whisper and the Nemotron
// sidecar over the same audio and returns combined, model-tagged variants,
// each ranked best-first within its model. Whisper and Nemotron live in
// separate processes (separate CUDA contexts), so the two recognitions
// overlap on the GPU — we launch Nemotron in a goroutine and run Whisper
// while it works.
type engineManager struct {
	whisper      app.Transcriber      // single-pass Whisper producer (always set)
	whisperMulti app.MultiTranscriber // Whisper batch producer; nil when count==1
	nemo         *nemotron.Client
}

func (m *engineManager) Count() int {
	if m.whisperMulti != nil {
		return m.whisperMulti.Count()
	}
	return 1
}

func (m *engineManager) Dictate(pcm []float32, leadOffsetSec float64, multi bool) []app.Variant {
	n := 1
	if multi && m.whisperMulti != nil {
		n = m.whisperMulti.Count()
	}

	// Nemotron in the background — it's a separate process, so it runs
	// concurrently with the in-process Whisper pass below.
	var nemoVars []app.Variant
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cands, err := m.nemo.Recognize(pcm, n)
		if err != nil {
			log.Printf("nemotron: %v", err)
			return
		}
		for _, c := range cands {
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			nemoVars = append(nemoVars, app.Variant{
				Text:       c.Text,
				Model:      app.ModelNemotron,
				Score:      nemotron.HybridScore(c.Text, c.Score),
				Confidence: c.Score,
			})
		}
		sort.SliceStable(nemoVars, func(i, j int) bool {
			return nemoVars[i].Score > nemoVars[j].Score
		})
	}()

	var whisperVars []app.Variant
	if multi && m.whisperMulti != nil {
		whisperVars = m.whisperMulti.Run(pcm, leadOffsetSec)
	} else if txt, err := m.whisper.Transcribe(pcm); err != nil {
		log.Printf("whisper: %v", err)
	} else if strings.TrimSpace(txt) != "" {
		whisperVars = []app.Variant{{Text: txt}}
	}
	for i := range whisperVars {
		whisperVars[i].Model = app.ModelWhisper
	}

	wg.Wait()
	return append(whisperVars, nemoVars...)
}

// setupNemotron builds the cross-engine (Whisper producers + Nemotron sidecar
// client) and wires the Break / Ctrl+Break keys. Returns the CrossEngine to
// plug into app.Config.CrossEngine. The client is returned even when the
// sidecar isn't up yet — a failed socket connection surfaces as a transcribe
// error at dictation time (logged), so the engine "exists but is asleep"
// until the systemd service finishes loading the model. Hotkey-registration
// failure is non-fatal.
func setupNemotron(events chan app.Event, whisper app.Transcriber, whisperMulti app.MultiTranscriber) app.CrossEngine {
	sock := os.Getenv("MURRLY_NEMOTRON_SOCK")
	if sock == "" {
		sock = fmt.Sprintf("/run/user/%d/murrly-nemotron.sock", os.Getuid())
	}
	client := nemotron.New(nemotron.Config{SocketPath: sock, Lang: "ru-RU", Alpha: 0.5})
	mgr := &engineManager{whisper: whisper, whisperMulti: whisperMulti, nemo: client}

	// Break: record → both engines → insert best Nemotron.
	if bhk, err := hotkey.New("pause"); err != nil {
		log.Printf("nemotron: Break hotkey unavailable: %v", err)
	} else {
		go bhk.Start()
		go func() {
			for e := range bhk.Events() {
				switch e {
				case hotkey.EventDown:
					trySend(events, app.EventKeyDownNemotron)
				case hotkey.EventUp:
					trySend(events, app.EventKeyUpNemotron)
				}
			}
		}()
		log.Printf("nemotron: Break wired to sidecar at %s", sock)
	}

	// Ctrl+Break: reprocess → both engines → insert best Nemotron.
	if rhk, err := hotkey.NewWithCtrl("pause"); err != nil {
		log.Printf("nemotron: Ctrl+Break hotkey unavailable: %v", err)
	} else {
		go rhk.Start()
		go func() {
			for e := range rhk.Events() {
				if e == hotkey.EventDown {
					trySend(events, app.EventReprocessNemotron)
				}
			}
		}()
	}

	return mgr
}

// trySend posts an event without blocking — drops it if the buffer is full
// (the user can just press again), matching the other hotkey pumps.
func trySend(events chan app.Event, ev app.Event) {
	select {
	case events <- ev:
	default:
	}
}
