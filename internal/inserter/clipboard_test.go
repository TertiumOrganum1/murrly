package inserter

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, s)
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type fakeClipboard struct {
	log      *callLog
	set      string
	saveErr  error
	restored bool
}

func (c *fakeClipboard) Save() (any, error) {
	c.log.add("save")
	return "snapshot", c.saveErr
}

func (c *fakeClipboard) Set(text string) error {
	c.log.add("set")
	c.set = text
	return nil
}

func (c *fakeClipboard) Restore(any) error {
	c.log.add("restore")
	c.restored = true
	return nil
}

func (c *fakeClipboard) ServesText(string) bool { return true }

type fakePaster struct {
	log *callLog
	err error
}

func (p *fakePaster) Paste(beforeKey func()) error {
	beforeKey()
	p.log.add("paste")
	return p.err
}

func TestClipboardRouteOrdersTheWholeDance(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg}
	pa := &fakePaster{log: lg}

	r := &Clipboard{CB: cb, Paster: pa, PasteDelay: time.Millisecond}
	if err := r.Insert("диктовка"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	want := []string{"save", "set", "paste", "restore"}
	got := lg.snapshot()
	if len(got) != len(want) {
		t.Fatalf("call order: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call order: got %v, want %v", got, want)
		}
	}
	if cb.set != "диктовка" {
		t.Errorf("clipboard got %q", cb.set)
	}
}

// TestClipboardRouteRestoresAfterAFailedPaste is what makes this route safe
// as a fallback: if the paste chord fails we still owe the user their
// clipboard back, and the chain still needs to hear that we failed.
func TestClipboardRouteRestoresAfterAFailedPaste(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg}
	pa := &fakePaster{log: lg, err: errors.New("no xdotool")}

	err := (&Clipboard{CB: cb, Paster: pa, PasteDelay: time.Millisecond}).Insert("текст")
	if err == nil {
		t.Fatal("Insert reported success after the paste failed")
	}
	if !cb.restored {
		t.Error("clipboard left displaced after a failed paste")
	}
}

// TestClipboardReplaceModeSkipsSaveAndRestore pins the second mode: publish,
// paste, and leave the dictation in the clipboard. No snapshot to stall on
// and no restore for a late reader to pick up instead of the text.
func TestClipboardReplaceModeSkipsSaveAndRestore(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg}
	pa := &fakePaster{log: lg}

	r := &Clipboard{CB: cb, Paster: pa, PasteDelay: time.Millisecond, Replace: true}
	if err := r.Insert("диктовка"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	for _, c := range lg.snapshot() {
		if c == "save" || c == "restore" {
			t.Fatalf("replacing mode touched the user's clipboard: %v", lg.snapshot())
		}
	}
	if cb.set != "диктовка" {
		t.Errorf("clipboard got %q", cb.set)
	}
}

// budgetedClipboard is a fakeClipboard that also honours the bounded read
// the replacing route prefers (inserter.snapshotBackend).
type budgetedClipboard struct {
	fakeClipboard
	budget time.Duration
}

func (c *budgetedClipboard) SaveWithin(d time.Duration) (any, error) {
	c.budget = d
	return c.Save()
}

func TestClipboardReplaceModeStashesWhatItOverwrites(t *testing.T) {
	lg := &callLog{}
	cb := &budgetedClipboard{fakeClipboard: fakeClipboard{log: lg}}
	pa := &fakePaster{log: lg}
	var displaced any
	r := &Clipboard{CB: cb, Paster: pa, PasteDelay: time.Millisecond, Replace: true,
		OnDisplaced: func(prev any) { displaced = prev }}

	if err := r.Insert("диктовка"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if displaced != "snapshot" {
		t.Errorf("displaced content = %v, want the snapshot taken before Set", displaced)
	}
	if cb.budget != displacedSnapshotWait {
		t.Errorf("snapshot read budget = %v, want %v", cb.budget, displacedSnapshotWait)
	}
	// The snapshot has to happen before the overwrite, or it snapshots our
	// own dictation; and it must not turn into a restore afterwards.
	want := []string{"save", "set", "paste"}
	got := lg.snapshot()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

// A backend that cannot answer within the budget must not stop the insert:
// the dictation still lands, there is just nothing to offer in the menu.
func TestClipboardReplaceModeInsertsWhenSnapshotFails(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg, saveErr: errors.New("hung selection owner")}
	pa := &fakePaster{log: lg}
	called := false
	r := &Clipboard{CB: cb, Paster: pa, PasteDelay: time.Millisecond, Replace: true,
		OnDisplaced: func(any) { called = true }}

	if err := r.Insert("диктовка"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if called {
		t.Error("a failed snapshot was handed to OnDisplaced anyway")
	}
	if cb.set != "диктовка" {
		t.Errorf("clipboard got %q, want the dictation inserted regardless", cb.set)
	}
}
