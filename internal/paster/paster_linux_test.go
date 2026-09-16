//go:build linux

package paster

import (
	"os"
	"testing"
)

// The chord that goes to a terminal has to be Ctrl+Shift+V: in a terminal
// Ctrl+V is the readline/VTE "quoted insert", which puts a literal ^V on the
// line and eats the paste. These pin the decision against the classes the
// emulators on this desktop actually report — konsole answers
// "x-terminal-emulator" as its instance and "konsole" as its class, so both
// halves of WM_CLASS have to be consulted.
func TestTerminalIsRecognisedFromEitherHalfOfWMClass(t *testing.T) {
	cases := []struct {
		name     string
		instance string
		class    string
		want     bool
	}{
		{"konsole", "x-terminal-emulator", "konsole", true},
		{"gnome-terminal", "gnome-terminal-server", "Gnome-terminal", true},
		{"xterm", "xterm", "XTerm", true},
		{"alacritty", "alacritty", "Alacritty", true},
		// VS Code's integrated terminal lives in a window of class "code",
		// where Ctrl+V is an ordinary paste and adding Shift would open the
		// markdown preview instead.
		{"vs code", "code", "Code", false},
		{"chrome", "google-chrome", "Google-chrome", false},
		{"a window with no class at all", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isTerminalClass(c.instance, c.class)
			if got != c.want {
				t.Fatalf("isTerminalClass(%q, %q) = %v, want %v",
					c.instance, c.class, got, c.want)
			}
		})
	}
}

func TestParseWMClass(t *testing.T) {
	cases := []struct {
		name            string
		raw             string
		instance, class string
	}{
		{"the usual pair", "konsole\x00Konsole\x00", "konsole", "Konsole"},
		{"no trailing NUL", "konsole\x00Konsole", "konsole", "Konsole"},
		{"instance only", "konsole\x00", "konsole", ""},
		{"empty property", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			instance, class := parseWMClass([]byte(c.raw))
			if instance != c.instance || class != c.class {
				t.Fatalf("parseWMClass(%q) = (%q, %q), want (%q, %q)",
					c.raw, instance, class, c.instance, c.class)
			}
		})
	}
}

// The property read is the half that silently broke before: the old code
// shelled out to `xdotool getwindowclassname`, which does not exist in the
// xdotool Debian and Ubuntu ship, so every terminal paste got a plain Ctrl+V.
// This does not assert WHICH window is active — that is whatever is on screen
// — only that reading the class off the live X server works at all.
func TestActiveWindowClassReadsTheLiveServer(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	instance, class, ok := activeWindowClass()
	if !ok {
		t.Skip("no active window to read (headless session or an unmanaged focus)")
	}
	if instance == "" && class == "" {
		t.Fatal("read the active window but both halves of WM_CLASS were empty")
	}
	t.Logf("active window WM_CLASS = (%q, %q), terminal=%v",
		instance, class, isTerminalClass(instance, class))
}
