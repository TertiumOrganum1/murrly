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
	secs := 30
	if s := os.Getenv("UICONTEXT_PROBE_SECS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			secs = n
		}
	}
	deadline := time.After(time.Duration(secs) * time.Second)
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

// TestLiveTree dumps the FULL AT-SPI subtree under every focused element in
// the active window — role, char count, caret, and a text snippet at each
// node. It answers: is the typed text hidden somewhere DEEPER in the tree
// than readContext drills, or is it absent from accessibility entirely?
//
//	UICONTEXT_LIVE=1 UICONTEXT_DELAY=7 go test -run TestLiveTree -v ./internal/uicontext/
func TestLiveTree(t *testing.T) {
	if os.Getenv("UICONTEXT_LIVE") == "" {
		t.Skip("set UICONTEXT_LIVE=1")
	}
	// Poll for up to ~25s until the active window holds a focused EDITABLE
	// element in-rect (the input box), then dump. This removes the
	// guess-the-timing problem: just click into the target field within the
	// window and the dump fires on the right frame automatically.
	conn := getOrDie(t)
	var b strings.Builder
	deadline := time.Now().Add(25 * time.Second)
	t.Log("polling up to 25s — click into the target input field and hold")
	for {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pid, rect, err := activeWindow(dctx)
		if err != nil {
			cancel()
			break
		}
		c := &atspiClient{ctx: dctx, conn: conn}
		apps, _ := c.appsForPID(pid)
		var focusedEditable []ref
		for _, app := range apps {
			matches, _ := c.focusedIn(app)
			for _, m := range matches {
				if c.isEditable(m) {
					focusedEditable = append(focusedEditable, m)
				}
			}
		}
		if len(focusedEditable) > 0 {
			fmt.Fprintf(&b, "pid=%d rect=%+v\n", pid, rect)
			for _, m := range focusedEditable {
				fmt.Fprintf(&b, "FOCUSED-EDITABLE busname=%s path=%s:\n", m.Name, m.Path)
				c.dumpTree(&b, m, 0)
			}
			cancel()
			break
		}
		cancel()
		if time.Now().After(deadline) {
			b.WriteString("no focused-editable element seen within 25s\n")
			break
		}
		time.Sleep(700 * time.Millisecond)
	}
	out := os.Getenv("UICONTEXT_LIVE_OUT")
	if out != "" {
		_ = os.WriteFile(out, []byte(b.String()), 0o644)
	}
	t.Log("\n" + b.String())
}

// dumpTree recursively prints role/chars/caret/snippet for a node and its
// descendants, depth- and breadth-limited so a runaway tree can't hang.
func (c *atspiClient) dumpTree(b *strings.Builder, node ref, depth int) {
	if depth > 8 {
		return
	}
	role, _ := c.roleName(node)
	cnt, _ := c.charCount(node)
	caret, _ := c.caretOffset(node)
	snip := ""
	if cnt > 0 {
		n := cnt
		if n > 60 {
			n = 60
		}
		if tx, err := c.getText(node, 0, n); err == nil {
			snip = strconv.Quote(tx)
		}
	}
	fmt.Fprintf(b, "%s%s role=%s chars=%d caret=%d editable=%v snip=%s\n",
		strings.Repeat("  ", depth+1), node.Path, role, cnt, caret, c.isEditable(node), snip)
	var children []ref
	if err := c.call(node, ifaceAccessible+".GetChildren").Store(&children); err != nil {
		return
	}
	for i, ch := range children {
		if i >= 12 {
			fmt.Fprintf(b, "%s… (%d more children)\n", strings.Repeat("  ", depth+2), len(children)-12)
			break
		}
		c.dumpTree(b, ch, depth+1)
	}
}

// TestLiveScan walks EVERY app on the a11y bus and prints all editable text
// fields with their busname/path/role and a text snippet — NO focus needed,
// the user holds nothing. Finds the Codex / webview chat input wherever it is and
// shows whether its typed text reaches accessibility.
//
//	UICONTEXT_LIVE=1 go test -run TestLiveScan -v ./internal/uicontext/
func TestLiveScan(t *testing.T) {
	if os.Getenv("UICONTEXT_LIVE") == "" {
		t.Skip("set UICONTEXT_LIVE=1")
	}
	dctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	conn := getOrDie(t)
	c := &atspiClient{ctx: dctx, conn: conn}
	var apps []ref
	if err := c.call(ref{registryName, registryRoot}, ifaceAccessible+".GetChildren").Store(&apps); err != nil {
		t.Fatalf("desktop children: %v", err)
	}
	var b strings.Builder
	for _, app := range apps {
		name := c.nameOf(app)
		var hits strings.Builder
		c.scanEditable(&hits, app, 0)
		if hits.Len() > 0 {
			fmt.Fprintf(&b, "APP %q (%s):\n%s", name, app.Name, hits.String())
		}
	}
	out := os.Getenv("UICONTEXT_LIVE_OUT")
	if out != "" {
		_ = os.WriteFile(out, []byte(b.String()), 0o644)
	}
	t.Log("\n" + b.String())
}

func (c *atspiClient) nameOf(r ref) string {
	var v dbus.Variant
	if err := c.call(r, "org.freedesktop.DBus.Properties.Get", ifaceAccessible, "Name").Store(&v); err != nil {
		return ""
	}
	s, _ := v.Value().(string)
	return s
}

// scanEditable recursively prints every editable Text node under root.
func (c *atspiClient) scanEditable(b *strings.Builder, node ref, depth int) {
	if depth > 14 {
		return
	}
	if c.hasText(node) && c.isEditable(node) {
		role, _ := c.roleName(node)
		cnt, _ := c.charCount(node)
		caret, _ := c.caretOffset(node)
		snip := ""
		if cnt > 0 {
			n := cnt
			if n > 80 {
				n = 80
			}
			if tx, err := c.getText(node, 0, n); err == nil {
				snip = strconv.Quote(tx)
			}
		}
		fmt.Fprintf(b, "  EDITABLE busname=%s path=%s role=%s chars=%d caret=%d snip=%s\n",
			node.Name, node.Path, role, cnt, caret, snip)
	}
	var children []ref
	if err := c.call(node, ifaceAccessible+".GetChildren").Store(&children); err != nil {
		return
	}
	for i, ch := range children {
		if i >= 40 {
			break
		}
		c.scanEditable(b, ch, depth+1)
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
