//go:build linux

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/tertiumorganum1/murrly/internal/app"
	"github.com/tertiumorganum1/murrly/internal/hotkey"
	"github.com/tertiumorganum1/murrly/internal/nemotron"
)

// setupNemotron builds the Nemotron sidecar client and wires the Break key
// to drive it (EventKeyDownNemotron / EventKeyUpNemotron). Returns the
// transcriber to plug into app.Config.NemotronTranscriber.
//
// The client is returned even when the sidecar isn't running yet: a failed
// connection surfaces as a transcribe error at dictation time (logged, no
// text inserted), so the engine "exists but is asleep" until the sidecar
// comes up. Break-hotkey registration failure is non-fatal — it just means
// the engine can't be triggered.
func setupNemotron(events chan app.Event) app.Transcriber {
	sock := os.Getenv("MURRLY_NEMOTRON_SOCK")
	if sock == "" {
		sock = fmt.Sprintf("/run/user/%d/murrly-nemotron.sock", os.Getuid())
	}
	client := nemotron.New(nemotron.Config{
		SocketPath: sock,
		Lang:       "ru-RU",
		Alpha:      0.5,
	})

	bhk, err := hotkey.New("pause")
	if err != nil {
		log.Printf("nemotron: Break hotkey unavailable: %v — engine wired but untriggerable", err)
		return client
	}
	go bhk.Start()
	go func() {
		for e := range bhk.Events() {
			switch e {
			case hotkey.EventDown:
				select {
				case events <- app.EventKeyDownNemotron:
				default:
				}
			case hotkey.EventUp:
				select {
				case events <- app.EventKeyUpNemotron:
				default:
				}
			}
		}
	}()
	log.Printf("nemotron: Break wired to sidecar at %s", sock)
	return client
}
