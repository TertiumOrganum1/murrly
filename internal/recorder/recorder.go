// Package recorder captures audio from the default input device into a mono
// 16 kHz float32 PCM buffer between Start and Stop.
package recorder

import (
	"fmt"
	"sync"

	"github.com/gordonklaus/portaudio"
)

const sampleRate = 16000

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
