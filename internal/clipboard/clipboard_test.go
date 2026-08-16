//go:build linux

package clipboard

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParseTargets(t *testing.T) {
	raw := "TARGETS\nTIMESTAMP\ntext/plain\ntext/html\nUTF8_STRING\nMULTIPLE\nSAVE_TARGETS\n"
	got := parseTargets(raw)
	want := []string{"text/plain", "text/html", "UTF8_STRING"}
	if len(got) != len(want) {
		t.Fatalf("got %d targets, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("targets[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSaveSetRestore is an integration test that requires xclip and a running X server.
// Skipped if xclip is not present or DISPLAY is unset.
func TestSaveSetRestore(t *testing.T) {
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip not available")
	}

	if err := setText("original-text"); err != nil {
		t.Fatalf("setText: %v", err)
	}

	c := New()
	saved, err := c.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := setText("replaced-text"); err != nil {
		t.Fatalf("setText replaced: %v", err)
	}
	if got := readTextSettled(t, "replaced-text"); got != "replaced-text" {
		t.Fatalf("after replace: got %q, want replaced-text", got)
	}
	if err := c.Restore(saved); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readTextSettled(t, "original-text"); got != "original-text" {
		t.Fatalf("after restore: got %q, want original-text", got)
	}
}

// readTextSettled polls the clipboard until it equals want or a short deadline
// elapses. X11 selection ownership is asynchronous: Set/Restore detach xclip
// via Start(), so the new owner may not be ready the instant we read.
func readTextSettled(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		out, err := readText()
		if err == nil {
			last = strings.TrimSpace(out)
			if last == want {
				return last
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last
}

// TestParseRequestNumber covers the xclip -verbose stderr lines that
// WaitPasted keys on. xclip prints "  Waiting for selection request
// number N" after completing content transfer N-1 (TARGETS requests are
// served without incrementing the counter).
func TestParseRequestNumber(t *testing.T) {
	cases := []struct {
		line string
		n    int
		ok   bool
	}{
		{"  Waiting for selection request number 1", 1, true},
		{"  Waiting for selection request number 12", 12, true},
		{"Waiting for selection request number 3", 3, true},
		{"Waiting for selection requests, Control-C to quit", 0, false},
		{"Connected to X server.", 0, false},
		{"Using UTF8_STRING.", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := parseRequestNumber(c.line)
		if n != c.n || ok != c.ok {
			t.Errorf("parseRequestNumber(%q) = (%d, %v), want (%d, %v)", c.line, n, ok, c.n, c.ok)
		}
	}
}

// TestWaitPastedDetectsFetch pins WaitPasted's baseline semantics: only
// content fetches that happen AFTER the WaitPasted call count as the
// paste. Fetches before the call — Set's own confirmation read, or
// desktop clipboard snoopers that burst-read on every ownership change
// (observed live: two content fetches within moments of a Set) — must be
// absorbed. Uses the SECONDARY selection so live-desktop snoopers can't
// interfere and the user's real clipboard isn't clobbered. Requires
// xclip and a running X server.
func TestWaitPastedDetectsFetch(t *testing.T) {
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip not available")
	}

	c := New()
	owner, err := writeSelectionTracked("secondary", "wait-pasted-probe")
	if err != nil {
		t.Fatalf("writeSelectionTracked: %v", err)
	}
	c.owner = owner

	// A snooper-style fetch BEFORE WaitPasted is called…
	fetchSecondarySettled(t, "wait-pasted-probe")

	// …must not count as the paste: with no fetch inside the wait window
	// this has to time out.
	if c.WaitPasted(300 * time.Millisecond) {
		t.Fatal("WaitPasted counted a fetch that happened before the call")
	}

	// The real paste: a content fetch landing while WaitPasted blocks.
	timer := time.AfterFunc(300*time.Millisecond, func() {
		_ = exec.Command("xclip", "-selection", "secondary", "-o").Run()
	})
	defer timer.Stop()

	if !c.WaitPasted(2 * time.Second) {
		t.Fatal("WaitPasted did not detect the content fetch")
	}
}

// fetchSecondarySettled polls the SECONDARY selection until it serves want
// (the freshly-started xclip owner may need a moment to claim it).
func fetchSecondarySettled(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("xclip", "-selection", "secondary", "-o").Output()
		if err == nil && strings.TrimSpace(string(out)) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("secondary selection never served %q", want)
}

func setText(s string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func readText() (string, error) {
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	return string(out), err
}
