package inserter

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRoute struct {
	name   string
	err    error
	calls  int
	gotArg string
}

func (f *fakeRoute) Name() string { return f.name }

func (f *fakeRoute) Insert(text string) error {
	f.calls++
	f.gotArg = text
	return f.err
}

func TestChainStopsAtFirstSuccess(t *testing.T) {
	first := &fakeRoute{name: "atspi"}
	second := &fakeRoute{name: "type"}

	c := NewChain(first, second)
	if err := c.Insert("привет"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if first.calls != 1 || first.gotArg != "привет" {
		t.Errorf("first route: calls=%d arg=%q", first.calls, first.gotArg)
	}
	if second.calls != 0 {
		t.Errorf("second route ran even though the first succeeded (calls=%d)", second.calls)
	}
}

func TestChainFallsThroughFailures(t *testing.T) {
	first := &fakeRoute{name: "atspi", err: errors.New("no editable field")}
	second := &fakeRoute{name: "type"}

	c := NewChain(first, second)
	if err := c.Insert("текст"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if first.calls != 1 {
		t.Errorf("first route calls = %d, want 1", first.calls)
	}
	if second.calls != 1 || second.gotArg != "текст" {
		t.Errorf("fallback route: calls=%d arg=%q", second.calls, second.gotArg)
	}
}

// TestChainReportsEveryFailure matters for diagnosis: when nothing inserted
// the log has to say what each route complained about, not just the last one.
func TestChainReportsEveryFailure(t *testing.T) {
	first := &fakeRoute{name: "atspi", err: errors.New("no editable field")}
	second := &fakeRoute{name: "type", err: errors.New("xdotool missing")}

	err := NewChain(first, second).Insert("текст")
	if err == nil {
		t.Fatal("Insert returned nil when every route failed")
	}
	for _, want := range []string{"atspi", "no editable field", "type", "xdotool missing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestEmptyChainIsAnError(t *testing.T) {
	if err := NewChain().Insert("текст"); err == nil {
		t.Fatal("empty chain reported success without inserting anything")
	}
}

// TestForModeBuildsTheConfiguredChain pins the contract between the config
// value and the routes actually attempted — including that an unknown mode
// still inserts (via the most compatible chain) rather than doing nothing.
func TestForModeBuildsTheConfiguredChain(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"hybrid", "atspi→typing→clipboard"},
		{"atspi", "atspi"},
		{"type", "typing"},
		{"clipboard", "clipboard"},
		{"nonsense", "atspi→typing→clipboard"},
	}
	for _, c := range cases {
		got := ForMode(c.mode, 4, &fakeClipboard{log: &callLog{}}, &fakePaster{log: &callLog{}}, 0).Name()
		if got != c.want {
			t.Errorf("ForMode(%q) chain = %q, want %q", c.mode, got, c.want)
		}
	}
}

// TestForModeWithoutAClipboardBackendSkipsThatRoute keeps the chain honest
// on a build or config with no clipboard wired: a nil route would panic
// mid-dictation instead of falling through.
func TestForModeWithoutAClipboardBackendSkipsThatRoute(t *testing.T) {
	if got := ForMode("hybrid", 4, nil, nil, 0).Name(); got != "atspi→typing" {
		t.Errorf("chain without clipboard backend = %q, want %q", got, "atspi→typing")
	}
}

// TestChainSettlesOnceBeforeTheFirstRoute — the push-to-talk key release
// has to be waited out exactly once per dictation. Sleeping inside every
// route doubled the delay whenever the first one fell through, which the
// user feels directly as lag before the text appears.
func TestChainSettlesOnceBeforeTheFirstRoute(t *testing.T) {
	first := &fakeRoute{name: "atspi", err: errors.New("nope")}
	second := &fakeRoute{name: "type"}

	c := NewChain(first, second)
	c.Settle = 40 * time.Millisecond

	start := time.Now()
	if err := c.Insert("текст"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Errorf("chain did not settle at all (%v)", elapsed)
	}
	if elapsed >= 80*time.Millisecond {
		t.Errorf("chain settled once per route (%v); it must settle once per insert", elapsed)
	}
}

func TestForModeSetsTheKeyReleaseSettle(t *testing.T) {
	if got := ForMode("hybrid", 4, nil, nil, 0).Settle; got <= 0 {
		t.Errorf("ForMode chain Settle = %v, want the key-release delay", got)
	}
}
