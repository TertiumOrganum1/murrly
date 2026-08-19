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

// TestWaitPastedDetectsFetch pins the arming semantics: only fetches that
// land after ArmPasteWait (which the paster calls immediately before the
// keystroke) count as the paste. Fetches before it — Set's own
// confirmation read, or the desktop clipboard snoopers that burst-read on
// every ownership change (observed live: two content fetches moments after
// a Set) — must be absorbed. Uses the SECONDARY selection so live-desktop
// snoopers can't interfere and the user's real clipboard isn't clobbered.
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

	// A snooper-style fetch BEFORE arming…
	fetchSecondarySettled(t, "wait-pasted-probe")
	c.ArmPasteWait()

	// …must not count: with no fetch after the mark this has to time out.
	if c.WaitPasted(300 * time.Millisecond) {
		t.Fatal("WaitPasted counted a fetch that happened before arming")
	}

	// The real paste: a content fetch landing after the mark. Arm first,
	// exactly as the paster does right before pressing the keys.
	c.ArmPasteWait()
	timer := time.AfterFunc(200*time.Millisecond, func() {
		_ = exec.Command("xclip", "-selection", "secondary", "-o").Run()
	})
	defer timer.Stop()

	if !c.WaitPasted(2 * time.Second) {
		t.Fatal("WaitPasted did not detect the content fetch")
	}
}

// TestRestoreSurvivesAHostileClaim pins the other half of the deal: the
// user's clipboard must come back even when something else claims the
// selection right as our dictation owner gives it up. Restore verifies its
// own work and re-publishes when the read-back shows a different owner won.
func TestRestoreSurvivesAHostileClaim(t *testing.T) {
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip not available")
	}

	c := New()
	// Dictation owns the clipboard, the user's text is what we saved.
	if err := c.Set("DICTATION-TEXT"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	saved := Saved{HasContent: true, Text: "USER-CLIPBOARD"}

	// A hostile owner grabs the selection just after Restore publishes,
	// the way a clipboard manager preserving the dying owner's content
	// would.
	timer := time.AfterFunc(60*time.Millisecond, func() {
		_ = writeSelection("clipboard", "HOSTILE-CLAIM")
	})
	defer timer.Stop()

	if err := c.Restore(saved); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := readTextSettled(t, "USER-CLIPBOARD"); got != "USER-CLIPBOARD" {
		t.Fatalf("after Restore: got %q, want USER-CLIPBOARD", got)
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
