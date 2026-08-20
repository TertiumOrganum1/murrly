//go:build linux

package uicontext

import (
	"context"
	"os"
	"strconv"
	"strings"
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

// TestLiveFocusInterfaces dumps every focused candidate in the active window
// with the AT-SPI interfaces it actually implements, and says which one
// InsertAtCaret would write to. Read-only: it never inserts anything.
//
// This is the tool for answering "why did the atspi route refuse here" —
// whether the field is missing from the accessibility tree entirely, or it
// is there but exposes no EditableText.
//
//	UICONTEXT_LIVE=1 UICONTEXT_DELAY=5 go test ./internal/uicontext/ -run TestLiveFocusInterfaces -v
func TestLiveFocusInterfaces(t *testing.T) {
	if os.Getenv("UICONTEXT_LIVE") == "" {
		t.Skip("set UICONTEXT_LIVE=1 to inspect the live desktop")
	}
	if d := os.Getenv("UICONTEXT_DELAY"); d != "" {
		if secs, err := strconv.Atoi(d); err == nil {
			t.Logf("waiting %ds — click into the field you want to inspect", secs)
			time.Sleep(time.Duration(secs) * time.Second)
		}
	}

	ensureFocusTracker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pid, rect, err := activeWindow(ctx)
	if err != nil {
		t.Fatalf("active window: %v", err)
	}
	t.Logf("active window: pid=%d rect=%+v", pid, rect)

	conn, err := getA11yConn()
	if err != nil {
		t.Fatalf("a11y bus: %v", err)
	}
	c := &atspiClient{ctx: ctx, conn: conn}

	apps, err := c.appsForPID(pid)
	if err != nil {
		t.Fatalf("apps for pid: %v", err)
	}
	t.Logf("accessible apps for this pid: %d", len(apps))

	total := 0
	for _, app := range apps {
		matches, err := c.focusedIn(app)
		if err != nil {
			t.Logf("app %s: focus query failed: %v", app.Name, err)
			continue
		}
		for _, m := range matches {
			total++
			role, _ := c.roleName(m)
			n, _ := c.charCount(m)
			var ifaces []string
			_ = c.call(m, ifaceAccessible+".GetInterfaces").Store(&ifaces)
			x, y, w, h, okExt := c.extents(m)
			t.Logf("candidate %s%s\n    role=%q editable=%v chars=%d extents=(%d,%d %dx%d ok=%v)\n    interfaces=%v",
				m.Name, m.Path, role, c.isEditable(m), n, x, y, w, h, okExt, ifaces)
		}
	}
	if total == 0 {
		t.Log("NO focused element is exposed at all — the atspi route cannot work in this app")
	}

	if target, ok := c.insertTarget(pid, rect); ok {
		var ifaces []string
		_ = c.call(target, ifaceAccessible+".GetInterfaces").Store(&ifaces)
		t.Logf("insertTarget picks %s%s with interfaces %v", target.Name, target.Path, ifaces)

		// Introspect the node so a signature mismatch can be told apart
		// from a missing interface: the D-Bus error for both is the same
		// "method ... doesn't exist".
		var xml string
		if err := c.call(target, "org.freedesktop.DBus.Introspectable.Introspect").Store(&xml); err != nil {
			t.Logf("introspect failed: %v", err)
		} else if i := strings.Index(xml, ifaceEditableText); i < 0 {
			t.Log("the target node does NOT implement EditableText — nothing to insert through")
		} else {
			tail := xml[i:]
			if j := strings.Index(tail, "</interface>"); j > 0 {
				tail = tail[:j]
			}
			t.Logf("EditableText as this node declares it:\n%s", tail)
		}
	} else {
		t.Log("insertTarget: no editable candidate — atspi route refuses, chain falls through to typing")
	}
}
