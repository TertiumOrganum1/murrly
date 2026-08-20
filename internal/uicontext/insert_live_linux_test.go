//go:build linux

package uicontext

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// TestLiveInsert writes a marker string into whatever field currently has
// the focus, so the AT-SPI route can be checked against real applications
// (VS Code, a browser, a terminal, a GTK/Qt app) without dictating.
//
// It is skipped unless UICONTEXT_LIVE=1 because it types into the user's
// actual focused field — running it unattended would drop text into
// whatever window happened to be in front.
//
//	UICONTEXT_LIVE=1 UICONTEXT_DELAY=5 go test ./internal/uicontext/ -run TestLiveInsert -v
//
// UICONTEXT_DELAY gives you N seconds to click into the target field first.
func TestLiveInsert(t *testing.T) {
	if os.Getenv("UICONTEXT_LIVE") == "" {
		t.Skip("set UICONTEXT_LIVE=1 to insert into the live desktop")
	}
	if d := os.Getenv("UICONTEXT_DELAY"); d != "" {
		if secs, err := strconv.Atoi(d); err == nil {
			t.Logf("waiting %ds — click into the field you want to test", secs)
			time.Sleep(time.Duration(secs) * time.Second)
		}
	}
	text := os.Getenv("UICONTEXT_TEXT")
	if text == "" {
		text = "проверка вставки"
	}

	start := time.Now()
	err := InsertAtCaret(text)
	t.Logf("InsertAtCaret(%q) took %v", text, time.Since(start))
	if err != nil {
		t.Fatalf("insert failed (this field would fall through to the typing route): %v", err)
	}
	t.Log("inserted — check that the text landed in the field you focused, at the caret")
}
