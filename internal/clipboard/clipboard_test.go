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

func setText(s string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func readText() (string, error) {
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	return string(out), err
}
