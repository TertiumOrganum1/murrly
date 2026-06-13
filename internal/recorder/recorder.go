// Package recorder captures audio from an input device into a mono 16 kHz
// float32 PCM buffer between Start and Stop. By default it uses the system
// default input; a device can be pinned by name via SetInputDevice (config
// [audio] device) so a change of the OS default (e.g. a wireless mic going
// active) doesn't silently switch capture to a dead device.
package recorder

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/gordonklaus/portaudio"
)

const sampleRate = 16000

// inputDeviceName, when non-empty, pins capture to the first input device
// whose name contains it (case-insensitive). Set once at startup.
var inputDeviceName string

// SetInputDevice pins the capture device by name substring. Empty = OS default.
func SetInputDevice(name string) { inputDeviceName = strings.TrimSpace(name) }

// openInputStream opens a mono 16 kHz input stream on the pinned device, or
// the system default when none is pinned (or the pinned one isn't found).
func openInputStream(frame []float32) (*portaudio.Stream, error) {
	if inputDeviceName == "" {
		return portaudio.OpenDefaultStream(1, 0, sampleRate, len(frame), frame)
	}
	dev, err := findInputDevice(inputDeviceName)
	if err != nil {
		log.Printf("recorder: %v — falling back to default input", err)
		return portaudio.OpenDefaultStream(1, 0, sampleRate, len(frame), frame)
	}
	log.Printf("recorder: using input device %q [%s]", dev.Name, dev.HostApi.Name)
	params := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   dev,
			Channels: 1,
			Latency:  dev.DefaultLowInputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: len(frame),
	}
	return portaudio.OpenStream(params, frame)
}

// findInputDevice returns the first input-capable device whose name contains
// the given substring (case-insensitive). MME devices enumerate first and are
// the most permissive about arbitrary sample rates, so a bare name like
// "USB PnP" lands on the MME variant.
func findInputDevice(name string) (*portaudio.DeviceInfo, error) {
	devs, err := portaudio.Devices()
	if err != nil {
		return nil, err
	}
	lname := strings.ToLower(name)
	for _, d := range devs {
		if d.MaxInputChannels > 0 && strings.Contains(strings.ToLower(d.Name), lname) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("no input device matching %q", name)
}

type Recorder struct {
	mu     sync.Mutex
	stream *portaudio.Stream
	buf    []float32
	frame  []float32
	stopCh chan struct{}
	doneCh chan struct{}
}

func New() *Recorder { return &Recorder{} }

// InitPortAudio must be called once at program start before any Recorder is used.
// TerminatePortAudio must be called at shutdown.
func InitPortAudio() error      { return portaudio.Initialize() }
func TerminatePortAudio() error { return portaudio.Terminate() }

// Probe momentarily opens and closes a default input stream. On macOS,
// this is what surfaces the standard microphone permission prompt and
// registers the app under System Settings → Privacy → Microphone —
// without it, the prompt only appears the first time the user actually
// holds the push-to-talk hotkey, and the Privacy pane has no row for
// the app to toggle until then.
//
// Cheap and idempotent: after the user has granted (or denied) the
// permission, subsequent calls just open and close the stream without
// further prompts. Errors are returned for the caller to log but are
// not fatal — Denied or device-busy here is fine, the actual record
// path will surface them again at F12 time.
func Probe() error {
	// 1024-frame buffer is what Recorder.Start uses; reusing it keeps
	// the CoreAudio AUHAL config identical to the real-record path so
	// macOS sees this as the same client.
	frame := make([]float32, 1024)
	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, len(frame), frame)
	if err != nil {
		return fmt.Errorf("probe open: %w", err)
	}
	defer stream.Close()
	if err := stream.Start(); err != nil {
		return fmt.Errorf("probe start: %w", err)
	}
	return stream.Stop()
}

func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stream != nil {
		return fmt.Errorf("recorder: already running")
	}

	r.buf = r.buf[:0]
	r.frame = make([]float32, 1024)
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})

	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, len(r.frame), r.frame)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	if err := stream.Start(); err != nil {
		stream.Close()
		return fmt.Errorf("start stream: %w", err)
	}
	r.stream = stream

	go r.loop()
	return nil
}

func (r *Recorder) loop() {
	defer close(r.doneCh)
	for {
		select {
		case <-r.stopCh:
			return
		default:
			if err := r.stream.Read(); err != nil {
				return
			}
			r.mu.Lock()
			r.buf = append(r.buf, r.frame...)
			r.mu.Unlock()
		}
	}
}

// Stop ends recording and returns the captured PCM (mono, 16 kHz, float32 in [-1, 1]).
func (r *Recorder) Stop() ([]float32, error) {
	r.mu.Lock()
	if r.stream == nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("recorder: not running")
	}
	close(r.stopCh)
	r.mu.Unlock()

	<-r.doneCh
	if err := r.stream.Stop(); err != nil {
		return nil, fmt.Errorf("stop stream: %w", err)
	}
	if err := r.stream.Close(); err != nil {
		return nil, fmt.Errorf("close stream: %w", err)
	}

	r.mu.Lock()
	out := make([]float32, len(r.buf))
	copy(out, r.buf)
	r.stream = nil
	r.mu.Unlock()
	return out, nil
}

func SampleRate() int { return sampleRate }
