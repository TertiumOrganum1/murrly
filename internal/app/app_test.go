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
	deadline := time.Now().Add(4 * time.Second)
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
	ins := &recordingInserter{}
	st := &recordedStates{}
	var transcripts []string
	recordStarts := 0

	a := New(Config{
		Recorder:      rec,
		Transcriber:   tr,
		Inserter:      ins,
		OnRecordStart: func() { recordStarts++ },
		OnState:       st.Set,
		OnTranscript:  func(text string) { transcripts = append(transcripts, text) },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
	if got := ins.texts(); len(got) != 1 || got[0] != "hello world" {
		t.Errorf("inserted %v, want [hello world]", got)
	}
	// The clipboard snapshot is taken when the recording starts, not on the
	// insert path — that is the whole point of the hook.
	if recordStarts != 1 {
		t.Errorf("OnRecordStart fired %d times, want 1", recordStarts)
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
	ins := &recordingInserter{}
	st := &recordedStates{}
	calledTranscript := false

	a := New(Config{
		Recorder:     rec,
		Transcriber:  tr,
		Inserter:     ins,
		OnState:      st.Set,
		OnTranscript: func(string) { calledTranscript = true },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	events := make(chan Event, 4)
	go a.Run(ctx, events)

	events <- EventKeyDown
	time.Sleep(20 * time.Millisecond)
	events <- EventKeyUp
	waitUntilIdle(t, st)

	if ins.count() != 0 {
		t.Error("insert should not happen when text is empty")
	}
	if calledTranscript {
		t.Error("OnTranscript should not be called when text is empty")
	}
}

func TestEmptyRecordingSkipsTranscriptionAndPaste(t *testing.T) {
	rec := &fakeRecorder{}
	tr := &fakeTranscriber{output: "should not be used"}
	ins := &recordingInserter{}
	st := &recordedStates{}

	a := New(Config{
		Recorder:    rec,
		Transcriber: tr,
		Inserter:    ins,
		OnState:     st.Set,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
	if ins.count() != 0 {
		t.Error("insert should not happen when recording is empty")
	}
}

type recordingInserter struct {
	mu   sync.Mutex
	got  []string
	name string
}

func (r *recordingInserter) Insert(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, text)
	return nil
}

func (r *recordingInserter) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

func (r *recordingInserter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// TestSetInserterSwapsTheRouteAtRuntime backs the tray toggle: switching
// between direct input and the clipboard has to take effect on the next
// dictation, without a restart.
func TestSetInserterSwapsTheRouteAtRuntime(t *testing.T) {
	before := &recordingInserter{name: "before"}
	after := &recordingInserter{name: "after"}

	a := New(Config{
		Recorder:    &fakeRecorder{pcm: []float32{0.1}},
		Transcriber: &fakeTranscriber{output: "фраза"},
		Inserter:    before,
	})

	if err := a.insertText("первая"); err != nil {
		t.Fatalf("insertText: %v", err)
	}
	a.SetInserter(after)
	if err := a.insertText("вторая"); err != nil {
		t.Fatalf("insertText after swap: %v", err)
	}

	if before.count() != 1 {
		t.Errorf("original route got %d inserts, want 1", before.count())
	}
	if after.count() != 1 {
		t.Errorf("swapped-in route got %d inserts, want 1", after.count())
	}
}
