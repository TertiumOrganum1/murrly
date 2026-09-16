package inserter

import (
	"errors"
	"sync"
	"testing"
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
	log        *callLog
	published  string
	publishErr error
	releases   int
}

func (c *fakeClipboard) Publish(text string) (func(), error) {
	c.log.add("publish")
	if c.publishErr != nil {
		return func() {}, c.publishErr
	}
	c.published = text
	return func() {
		c.log.add("release")
		c.releases++
	}, nil
}

func (c *fakeClipboard) PublishAndHold(text string) error {
	c.log.add("publish")
	if c.publishErr != nil {
		return c.publishErr
	}
	c.published = text
	return nil
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

func wantCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

// The selection is kept rather than handed back — see the comment at the top
// of clipboard.go. Anything that comes for the dictation late is then answered
// by our own owner instead of by the desktop's clipboard manager.
func TestClipboardRoutePublishesAndPastesWithoutLettingGo(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg}
	pa := &fakePaster{log: lg}

	if err := (&Clipboard{CB: cb, Paster: pa}).Insert("диктовка"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	wantCalls(t, lg.snapshot(), []string{"publish", "paste"})
	if cb.published != "диктовка" {
		t.Errorf("clipboard got %q", cb.published)
	}
	if cb.releases != 0 {
		t.Errorf("released %d times, want 0 — the selection is kept", cb.releases)
	}
}

// A chord that never went out still has to be reported up the chain, so the
// next route in the hybrid chain gets its turn. The text stays in the clipboard
// either way: the user can paste it themselves, which is the whole fallback.
func TestClipboardRouteReportsAFailedPaste(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg}
	pa := &fakePaster{log: lg, err: errors.New("no xdotool")}

	if err := (&Clipboard{CB: cb, Paster: pa}).Insert("текст"); err == nil {
		t.Fatal("Insert reported success after the paste failed")
	}
}

// A clipboard that cannot be published to is a route failure, not a paste:
// pressing the chord anyway would paste whatever happened to be there.
func TestClipboardRouteDoesNotPasteWhenPublishFails(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg, publishErr: errors.New("no xclip")}
	pa := &fakePaster{log: lg}

	if err := (&Clipboard{CB: cb, Paster: pa}).Insert("текст"); err == nil {
		t.Fatal("Insert reported success after the publish failed")
	}
	wantCalls(t, lg.snapshot(), []string{"publish"})
}

// Empty text is a no-op rather than a clipboard wipe: a dictation that
// recognised nothing must not cost the user what they had copied.
func TestClipboardRouteIgnoresEmptyText(t *testing.T) {
	lg := &callLog{}
	cb := &fakeClipboard{log: lg}
	pa := &fakePaster{log: lg}

	if err := (&Clipboard{CB: cb, Paster: pa}).Insert(""); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	wantCalls(t, lg.snapshot(), nil)
}
