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

func (c *fakeClipboard) ArmPasteWait() { c.log.add("arm") }

func (c *fakeClipboard) WaitPasted(time.Duration) bool {
	c.log.add("wait")
	return true
}

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
	want := []string{"save", "set", "arm", "paste", "wait", "restore"}
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
