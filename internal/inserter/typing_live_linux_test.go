//go:build linux

package inserter

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLiveTyping types into whatever field currently has the focus, so the
// typing route can be measured against a real application: how long a long
// phrase takes, and whether the toolkit drops characters at the configured
// delay.
//
// Skipped unless INSERTER_LIVE=1 — it synthesises real keystrokes into the
// focused window, so running it unattended would scatter text into whatever
// happened to be in front.
//
//	INSERTER_LIVE=1 INSERTER_DELAY=5 go test ./internal/inserter/ -run TestLiveTyping -v
//
// INSERTER_TYPE_DELAY sets the per-keystroke pause (default 4 ms),
// INSERTER_TEXT overrides the phrase. Compare what lands in the field with
// the phrase the log prints: any difference is a dropped character.
func TestLiveTyping(t *testing.T) {
	if os.Getenv("INSERTER_LIVE") == "" {
		t.Skip("set INSERTER_LIVE=1 to type into the live desktop")
	}
	if d := os.Getenv("INSERTER_DELAY"); d != "" {
		if secs, err := strconv.Atoi(d); err == nil {
			t.Logf("waiting %ds — click into the field you want to test", secs)
			time.Sleep(time.Duration(secs) * time.Second)
		}
	}
	delay := 4
	if d := os.Getenv("INSERTER_TYPE_DELAY"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			delay = n
		}
	}
	text := os.Getenv("INSERTER_TEXT")
	if text == "" {
		// Long enough to expose dropped characters and to time honestly:
		// a real dictation is a paragraph, not a word.
		text = strings.Repeat("Проверка печати: съешь ещё этих мягких французских булок, да выпей чаю. ", 4)
	}

	start := time.Now()
	err := (&Typing{DelayMs: delay}).Insert(text)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("typing failed: %v", err)
	}
	t.Logf("typed %d characters at %d ms/key in %v", len([]rune(text)), delay, elapsed)
	t.Logf("expected text: %q", text)
	t.Log("compare the field's content with the line above — any difference is a dropped character")
}
