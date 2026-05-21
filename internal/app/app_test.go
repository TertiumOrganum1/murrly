package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeRecorder struct {
	mu      sync.Mutex
	started bool
	pcm     []float32
}

func (r *fakeRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
	return nil
}

func (r *fakeRecorder) Stop() ([]float32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = false
	return r.pcm, nil
}

type fakeTranscriber struct {
	mu     sync.Mutex
	output string
	called bool
}

func (t *fakeTranscriber) Transcribe(_ []float32) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.called = true
	return t.output, nil
}

type savedSnapshot struct{ text string }

type fakeClipboard struct {
	mu       sync.Mutex
	saved    savedSnapshot
	wasSaved bool
	pasted   string
	restored bool
}

func (c *fakeClipboard) Save() (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wasSaved = true
	return c.saved, nil
}

func (c *fakeClipboard) Set(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pasted = text
	return nil
}

func (c *fakeClipboard) Restore(_ any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restored = true
	return nil
}

type fakePaster struct {
	mu     sync.Mutex
	pasted bool
}

func (p *fakePaster) Paste() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pasted = true
	return nil
}

type recordedStates struct {
	mu     sync.Mutex
	states []State
}

func (r *recordedStates) Set(s State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *recordedStates) Snapshot() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]State, len(r.states))
	copy(out, r.states)
	return out
}

func (r *recordedStates) Contains(s State) bool {
	for _, x := range r.Snapshot() {
		if x == s {
			return true
		}
	}
	return false
}

func waitUntilIdle(t *testing.T, st *recordedStates) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		snap := st.Snapshot()
		if len(snap) >= 2 && snap[len(snap)-1] == StateIdle && snap[len(snap)-2] != StateIdle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never returned to idle; states=%v", st.Snapshot())
}

func TestHappyPath(t *testing.T) {
	rec := &fakeRecorder{pcm: []float32{0.1, 0.2, 0.3}}
	tr := &fakeTranscriber{output: "hello world"}
	cb := &fakeClipboard{}
	pa := &fakePaster{}
	st := &recordedStates{}
	var transcripts []string

	a := New(Config{
		Recorder:     rec,
		Transcriber:  tr,
		Clipboard:    cb,
		Paster:       pa,
		OnState:      st.Set,
		OnTranscript: func(text string) { transcripts = append(transcripts, text) },
		PasteDelay:   10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan Event, 4)
	go a.Run(ctx, events)

	events <- EventKeyDown
	time.Sleep(20 * time.Millisecond)
	events <- EventKeyUp
	waitUntilIdle(t, st)

	if !tr.called {
		t.Error("transcriber not called")
	}
	if cb.pasted != "hello world" {
		t.Errorf("clipboard text: got %q, want %q", cb.pasted, "hello world")
	}
	if !pa.pasted {
		t.Error("paste not invoked")
	}
	if !cb.restored {
		t.Error("clipboard not restored")
	}
	if len(transcripts) != 1 || transcripts[0] != "hello world" {
		t.Fatalf("transcripts = %v", transcripts)
	}
	if !st.Contains(StateRecording) {
		t.Error("never entered StateRecording")
	}
	if !st.Contains(StateTranscribing) {
		t.Error("never entered StateTranscribing")
	}
}

func TestEmptyTranscriptionSkipsPaste(t *testing.T) {
	rec := &fakeRecorder{pcm: []float32{0.1}}
	tr := &fakeTranscriber{output: ""}
	cb := &fakeClipboard{}
	pa := &fakePaster{}
	st := &recordedStates{}
	calledTranscript := false

	a := New(Config{
		Recorder:     rec,
		Transcriber:  tr,
		Clipboard:    cb,
		Paster:       pa,
		OnState:      st.Set,
		OnTranscript: func(string) { calledTranscript = true },
		PasteDelay:   10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan Event, 4)
	go a.Run(ctx, events)

	events <- EventKeyDown
	time.Sleep(20 * time.Millisecond)
	events <- EventKeyUp
	waitUntilIdle(t, st)

	if cb.wasSaved {
		t.Error("clipboard.Save should not be called when text is empty")
	}
	if pa.pasted {
		t.Error("paste should not happen when text is empty")
	}
	if calledTranscript {
		t.Error("OnTranscript should not be called when text is empty")
	}
}

func TestEmptyRecordingSkipsTranscriptionAndPaste(t *testing.T) {
	rec := &fakeRecorder{}
	tr := &fakeTranscriber{output: "should not be used"}
	cb := &fakeClipboard{}
	pa := &fakePaster{}
	st := &recordedStates{}

	a := New(Config{
		Recorder:    rec,
		Transcriber: tr,
		Clipboard:   cb,
		Paster:      pa,
		OnState:     st.Set,
		PasteDelay:  10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make(chan Event, 4)
	go a.Run(ctx, events)

	events <- EventKeyDown
	time.Sleep(20 * time.Millisecond)
	events <- EventKeyUp
	waitUntilIdle(t, st)

	if tr.called {
		t.Error("transcriber should not be called when recording is empty")
	}
	if cb.wasSaved {
		t.Error("clipboard.Save should not be called when recording is empty")
	}
	if pa.pasted {
		t.Error("paste should not happen when recording is empty")
	}
}
