//go:build linux

package uicontext

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// TestLiveCapture reads the REAL focused element on the developer's
// desktop — skipped unless explicitly requested, since the result
// depends on whatever window happens to be active:
//
//	UICONTEXT_LIVE=1 go test -run TestLiveCapture -v ./internal/uicontext/
func TestLiveCapture(t *testing.T) {
	if os.Getenv("UICONTEXT_LIVE") == "" {
		t.Skip("set UICONTEXT_LIVE=1 to probe the live desktop")
	}
	if d := os.Getenv("UICONTEXT_DELAY"); d != "" {
		if secs, err := strconv.Atoi(d); err == nil {
			t.Logf("waiting %ds — focus the target field and hold still", secs)
			time.Sleep(time.Duration(secs) * time.Second)
		}
	}
	ctx := Capture()
	t.Logf("Capture() = %+v", ctx)
	t.Logf("preceding=%q following=%q", ctx.Preceding, ctx.Following)
	// The harness may swallow test output — mirror it to a file when asked.
	if out := os.Getenv("UICONTEXT_LIVE_OUT"); out != "" {
		report := fmt.Sprintf("%+v\npreceding=%q following=%q\n%s",
			ctx, ctx.Preceding, ctx.Following, liveDiag())
		_ = os.WriteFile(out, []byte(report), 0o644)
	}
}

// TestLiveFocusSignals subscribes to the AT-SPI focus / caret signals for 30s
// and prints every one (sender, path, member, detail, resolved role+snippet).
// It answers the linchpin question for the VS Code field-selection bug: does
// Electron emit usable focus/caret events at all, and from which element?
//
//	UICONTEXT_PROBE=1 go test -run TestLiveFocusSignals -v ./internal/uicontext/
//
// Click into the chat input, then the text-file editor, then type a char in
// each, during the 30s window. The element that emits caret-moved / focused
// when you click is the live one (the phantom mirror never re-emits).
func TestLiveFocusSignals(t *testing.T) {
	if os.Getenv("UICONTEXT_PROBE") == "" {
		t.Skip("set UICONTEXT_PROBE=1 to probe live focus signals")
	}
	session, err := dbus.SessionBus()
	if err != nil {
		t.Fatalf("session bus: %v", err)
	}
	var addr string
	if err := session.Object("org.a11y.Bus", "/org/a11y/bus").
		Call("org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		t.Fatalf("a11y addr: %v", err)
	}
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("a11y connect: %v", err)
	}
	defer conn.Close()
	for _, m := range []string{
		"type='signal',interface='org.a11y.atspi.Event.Object',member='StateChanged'",
		"type='signal',interface='org.a11y.atspi.Event.Object',member='TextCaretMoved'",
		"type='signal',interface='org.a11y.atspi.Event.Focus'",
	} {
		conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, m)
	}
	ch := make(chan *dbus.Signal, 256)
	conn.Signal(ch)

	c := &atspiClient{ctx: context.Background(), conn: getOrDie(t)}
	deadline := time.After(30 * time.Second)
	out := os.Getenv("UICONTEXT_LIVE_OUT")
	var b strings.Builder
	t.Log("probing focus/caret signals for 30s — click into chat, then text file, then type")
	for {
		select {
		case <-deadline:
			if out != "" {
				_ = os.WriteFile(out, []byte(b.String()), 0o644)
			}
			return
		case sig := <-ch:
			member := sig.Name
			if i := strings.LastIndex(member, "."); i >= 0 {
				member = member[i+1:]
			}
			var detail string
			var d1 int32
			if len(sig.Body) >= 1 {
				detail, _ = sig.Body[0].(string)
			}
			if len(sig.Body) >= 2 {
				d1, _ = sig.Body[1].(int32)
			}
			// Only the interesting ones: focus gained, caret moved.
			if member == "StateChanged" && !(detail == "focused" && d1 == 1) {
				continue
			}
			r := ref{Name: sig.Sender, Path: sig.Path}
			role, _ := c.roleName(r)
			cnt, _ := c.charCount(r)
			caret, _ := c.caretOffset(r)
			snip := ""
			if cnt > 0 {
				n := cnt
				if n > 40 {
					n = 40
				}
				if tx, terr := c.getText(r, 0, n); terr == nil {
					snip = strconv.Quote(tx)
				}
			}
			line := fmt.Sprintf("[%s] %s sender=%s path=%s role=%s chars=%d caret=%d snip=%s",
				member, detail, sig.Sender, sig.Path, role, cnt, caret, snip)
			t.Log(line)
			b.WriteString(line + "\n")
		}
	}
}

func getOrDie(t *testing.T) *dbus.Conn {
	conn, err := getA11yConn()
	if err != nil {
		t.Fatalf("a11y conn: %v", err)
	}
	return conn
}

// liveDiag re-runs the capture pipeline step by step and reports what
// each stage saw — for debugging against the live desktop only.
func liveDiag() string {
	var b strings.Builder
	dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pid, rect, err := activeWindow(dctx)
	fmt.Fprintf(&b, "active pid: %d rect: %+v err=%v\n", pid, rect, err)
	conn, err := getA11yConn()
	if err != nil {
		fmt.Fprintf(&b, "a11y conn: %v\n", err)
		return b.String()
	}
	c := &atspiClient{ctx: dctx, conn: conn}
	apps, err := c.appsForPID(pid)
	fmt.Fprintf(&b, "apps for pid: %d err=%v\n", len(apps), err)
	for _, app := range apps {
		matches, err := c.focusedIn(app)
		fmt.Fprintf(&b, "  app %s %s: matches=%d err=%v\n", app.Name, app.Path, len(matches), err)
		for _, m := range matches {
			role, _ := c.roleName(m)
			cnt, cerr := c.charCount(m)
			caret, kerr := c.caretOffset(m)
			ex, ey, ew, eh, eok := c.extents(m)
			inRect := rect.known() && eok && rect.contains(ex+ew/2, ey+eh/2)
			snip := ""
			if cnt > 0 {
				n := cnt
				if n > 48 {
					n = 48
				}
				if t, terr := c.getText(m, 0, n); terr == nil {
					snip = strconv.Quote(t)
				}
			}
			fmt.Fprintf(&b, "    match %s role=%s chars=%d(%v) caret=%d(%v) editable=%v text=%v ext=(%d,%d,%d,%d) inRect=%v snip=%s\n",
				m.Path, role, cnt, cerr, caret, kerr, c.isEditable(m), c.hasText(m), ex, ey, ew, eh, inRect, snip)
		}
	}
	return b.String()
}
