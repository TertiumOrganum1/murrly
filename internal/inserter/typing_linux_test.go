//go:build linux

package inserter

import (
	"strings"
	"testing"
)

func TestTypeArgs(t *testing.T) {
	got := typeArgs(7, "привет мир")
	want := []string{"type", "--clearmodifiers", "--delay", "7", "--", "привет мир"}
	if len(got) != len(want) {
		t.Fatalf("typeArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("typeArgs = %v, want %v", got, want)
		}
	}
}

// TestTypeArgsPassesDashLeadingTextAsText guards the "--" terminator: a
// dictation that starts with a dash (" - потом", a dashed list) must reach
// the field as text, not be parsed as an xdotool flag.
func TestTypeArgsPassesDashLeadingTextAsText(t *testing.T) {
	got := typeArgs(4, "--delay 9000")
	if got[len(got)-1] != "--delay 9000" {
		t.Fatalf("dash-leading text mangled: %v", got)
	}
	if got[len(got)-2] != "--" {
		t.Fatalf("missing -- terminator before the text: %v", got)
	}
}

// TestTypeArgsClampsDelay keeps a nonsense config out of the command line:
// a negative delay would make xdotool error out and lose the dictation.
func TestTypeArgsClampsDelay(t *testing.T) {
	for _, d := range []int{-5, 0} {
		got := strings.Join(typeArgs(d, "x"), " ")
		if strings.Contains(got, "--delay -") || strings.Contains(got, "--delay 0 ") {
			t.Errorf("delay %d not clamped: %s", d, got)
		}
	}
}

func TestTypingRouteRejectsEmptyText(t *testing.T) {
	// Nothing to type is not a delivery failure — it must not send the
	// chain looking for another route.
	if err := (&Typing{}).Insert(""); err != nil {
		t.Errorf("empty text: got %v, want nil", err)
	}
}
