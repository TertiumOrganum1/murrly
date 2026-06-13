// audio-devices lists the audio host APIs and input devices PortAudio sees,
// marking the defaults. Diagnostic helper: run it when dictation records only
// silence to find which device/host API is actually the default and whether a
// working microphone is exposed.
//
//	go run ./cmd/audio-devices
package main

import (
	"fmt"

	"github.com/gordonklaus/portaudio"
)

func main() {
	if err := portaudio.Initialize(); err != nil {
		fmt.Println("Initialize:", err)
		return
	}
	defer portaudio.Terminate()

	defIn, _ := portaudio.DefaultInputDevice()
	apis, _ := portaudio.HostApis()
	for _, api := range apis {
		fmt.Printf("HostAPI: %s (default input: %v)\n", api.Name, devName(api.DefaultInputDevice))
	}
	fmt.Println()

	devs, err := portaudio.Devices()
	if err != nil {
		fmt.Println("Devices:", err)
		return
	}
	fmt.Println("Input devices (MaxInputChannels > 0):")
	for _, d := range devs {
		if d.MaxInputChannels <= 0 {
			continue
		}
		mark := "  "
		if defIn != nil && d.Name == defIn.Name && d.HostApi.Name == defIn.HostApi.Name {
			mark = "=>" // current default input
		}
		fmt.Printf("%s [%s] %q  in=%d  rate=%.0f\n",
			mark, d.HostApi.Name, d.Name, d.MaxInputChannels, d.DefaultSampleRate)
	}
}

func devName(d *portaudio.DeviceInfo) string {
	if d == nil {
		return "<none>"
	}
	return d.Name
}
