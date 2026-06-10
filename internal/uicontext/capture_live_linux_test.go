//go:build linux

package uicontext

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
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

// liveDiag re-runs the capture pipeline step by step and reports what
// each stage saw — for debugging against the live desktop only.
func liveDiag() string {
	var b strings.Builder
	dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pid, err := activeWindowPID(dctx)
	fmt.Fprintf(&b, "active pid: %d err=%v\n", pid, err)
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
			fmt.Fprintf(&b, "    match %s role=%s chars=%d(%v) caret=%d(%v) editable=%v text=%v\n",
				m.Path, role, cnt, cerr, caret, kerr, c.isEditable(m), c.hasText(m))
		}
	}
	return b.String()
}
