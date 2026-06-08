//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/hotkey"
	"github.com/tertiumorganum1/murrly/internal/menuactions"
	"github.com/tertiumorganum1/murrly/internal/nemotron"
	"github.com/tertiumorganum1/murrly/internal/nemotronservice"
)

// nemoVariants is the number of diversified variants the sidecar runs in
// multi mode (matches the sidecar's leading-silence list).
const nemoVariants = 4

// nemoSampleRateHz mirrors the recorder/sidecar sample rate, used to convert
// the reprocess leading-silence offset (seconds) into samples.
const nemoSampleRateHz = 16000

// engineManager implements app.NemotronEngine: Nemotron-only recognition over
// the sidecar socket, formatted (cyrillic→latin, capitalisation) and ranked
// by the hybrid scorer. Whisper is untouched — F12 keeps its own fast path;
// this only drives the Break key and the F12 background fill.
type engineManager struct {
	nemo *nemotron.Client
}

func (m *engineManager) Count() int { return nemoVariants }

func (m *engineManager) Run(pcm []float32, leadOffsetSec float64, multi bool) []app.Variant {
	// Honor the reprocess offset: prepend leading silence so a Ctrl+Break /
	// Ctrl+F12 round shifts the audio onto a fresh alignment and Nemotron
	// yields different variants instead of repeating the previous batch.
	if leadOffsetSec > 0 {
		pad := make([]float32, int(leadOffsetSec*nemoSampleRateHz))
		pcm = append(pad, pcm...)
	}
	n := 1
	if multi {
		n = nemoVariants
	}
	cands, err := m.nemo.Recognize(pcm, n)
	if err != nil {
		log.Printf("nemotron: %v", err)
		// If the sidecar is unreachable AND not currently up/loading, systemd
		// has given up retrying — kick a fresh restart so it's back for the
		// next dictation. If it's active (running or mid-load), leave it.
		if !nemotronservice.IsActive() {
			log.Printf("nemotron: sidecar not active — kicking a restart for next time")
			go func() { _ = nemotronservice.Restart() }()
		}
		return nil
	}
	out := make([]app.Variant, 0, len(cands))
	for _, c := range cands {
		formatted := nemotron.FormatNemotron(c.Text)
		if strings.TrimSpace(formatted) == "" {
			continue
		}
		out = append(out, app.Variant{
			Text:  formatted,
			Model: app.ModelNemotron,
			// Score the RAW text so the heuristics discriminate the cleanest
			// variant before formatting adds a uniform capital + period.
			Score:      nemotron.HybridScore(c.Text, c.Score),
			Confidence: c.Score,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// setupNemotron builds the Nemotron engine (sidecar client) and wires the
// Break / Ctrl+Break keys. Returns the engine for app.Config.Nemotron. The
// client is returned even when the sidecar isn't up yet — a failed socket
// connection just yields no variants (logged), so Break degrades to nothing
// inserted and the F12 background fill is skipped. Hotkey-registration
// failure is non-fatal.
func setupNemotron(events chan app.Event, ncfg config.NemotronConfig) app.NemotronEngine {
	if !ncfg.Enabled {
		log.Printf("nemotron: disabled in config")
		return nil
	}
	sock := ncfg.SocketPath
	if sock == "" {
		sock = os.Getenv("MURRLY_NEMOTRON_SOCK")
	}
	if sock == "" {
		sock = fmt.Sprintf("/run/user/%d/murrly-nemotron.sock", os.Getuid())
	}
	lang := ncfg.Lang
	if lang == "" {
		lang = "ru-RU"
	}
	client := nemotron.New(nemotron.Config{SocketPath: sock, Lang: lang, Alpha: ncfg.BoostAlpha})

	// Break: record → Nemotron → insert best Nemotron.
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

	// Ctrl+Break: reprocess the last audio through Nemotron, insert its best.
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

	return &engineManager{nemo: client}
}

// wireNemotronStatus hooks the tray's Nemotron group up to the live engine:
// the "Перезапустить Nemotron" action restarts the systemd sidecar, and
// NemotronStatus pings the socket so the tray can show загружается/готов/не
// запущен. No-op when the engine isn't an *engineManager (disabled).
func wireNemotronStatus(actions *menuactions.Actions, eng app.NemotronEngine) {
	mgr, ok := eng.(*engineManager)
	if !ok || mgr == nil {
		return
	}
	actions.OnRestartNemotron = func() {
		if err := nemotronservice.Restart(); err != nil {
			log.Printf("nemotron: restart failed: %v", err)
		}
	}
	actions.NemotronStatus = func() string {
		if mgr.nemo.Ping() == nil {
			return "готов ✓"
		}
		return "не запущен"
	}
}

// trySend posts an event without blocking — drops it if the buffer is full
// (the user can just press again), matching the other hotkey pumps.
func trySend(events chan app.Event, ev app.Event) {
	select {
	case events <- ev:
	default:
	}
}
