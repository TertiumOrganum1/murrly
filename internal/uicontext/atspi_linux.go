//go:build linux

// atspi_linux.go implements Capture over AT-SPI2 — the accessibility
// D-Bus that screen readers use — with no cgo: plain method calls via
// godbus against the dedicated a11y bus (address obtained from the
// session bus, org.a11y.Bus.GetAddress).
//
// Who exposes what (verified live on Mint 21.2 / Cinnamon / X11):
//   - GTK2/3 apps: full trees always, no setup.
//   - Qt apps (konsole, doublecmd): bridge activates at app START when
//     org.a11y.Status.IsEnabled is true; the persistent switch is
//     `gsettings org.cinnamon.desktop.interface toolkit-accessibility`
//     (see internal/a11ysetup). Telegram ships its own always-on Qt.
//   - Chromium/Electron (Chrome, VS Code): tree stays empty until the
//     app is told a screen reader exists — per-app enablement
//     (VS Code: editor.accessibilitySupport=on). Once on, it latches
//     for the process lifetime.
//   - Terminals (role "terminal") are deliberately skipped: the caret
//     semantics of a screen buffer are unverified, and mangling a
//     shell command line is worse than no transform.
//
// The path from "user released the hotkey" to a Context:
//  1. active window → PID (xdotool, already a paster dependency);
//  2. a11y-bus desktop children → the app(s) owning that PID;
//  3. Collection.GetMatches(STATE_FOCUSED) on the app root — the
//     toolkit walks its own tree, we never crawl it;
//  4. the focused element's Text interface: caret, selection, and the
//     characters around the insertion point;
//  5. Chromium-style rich fields embed children as U+FFFC placeholder
//     characters — resolve them by descending into the corresponding
//     child (paragraphs, inline spans), treating empty blocks and
//     block boundaries as line breaks.
package uicontext

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/godbus/dbus/v5"
)

const (
	ifaceAccessible = "org.a11y.atspi.Accessible"
	ifaceCollection = "org.a11y.atspi.Collection"
	ifaceText       = "org.a11y.atspi.Text"

	registryName = "org.a11y.atspi.Registry"
	registryRoot = dbus.ObjectPath("/org/a11y/atspi/accessible/root")

	// embedChar is AT-SPI's placeholder for a child object inside a
	// Text run (paragraphs and inline elements in Chromium fields).
	embedChar = '￼'

	// captureTimeout bounds the WHOLE capture (all D-Bus round-trips).
	// On overrun Capture degrades to HasContext=false — the paste then
	// goes through untransformed, same as before this feature.
	captureTimeout = 700 * time.Millisecond

	// stateActive / stateFocused / stateEditable are ATSPI_STATE_* enum
	// values used as bit positions in the state set's first word.
	// stateActive marks the active toplevel window.
	stateActive   = 1
	stateFocused  = 12
	stateEditable = 7

	// matchAll / matchNone are ATSPI_Collection_MATCH_* values used in
	// the GetMatches rule (ALL for the focused-state set, NONE for the
	// unused attribute/role/interface slots).
	matchAll  = int32(1)
	matchNone = int32(3)
	// sortCanonical is ATSPI_Collection_SORT_ORDER_CANONICAL.
	sortCanonical = uint32(1)

	// maxEmbedScan caps how much text we fetch when counting embed
	// placeholders to find a child's index — structured inputs (chat
	// boxes) are small; anything bigger is not worth drilling.
	maxEmbedScan = 20000
	// maxDescend caps the embed-drilling depth.
	maxDescend = 5
)

// ref is one AT-SPI object reference as it appears on the wire: the
// owning connection's unique bus name plus an object path ("(so)").
type ref struct {
	Name string
	Path dbus.ObjectPath
}

// a11yConn caches the connection to the dedicated accessibility bus.
// Re-dialed lazily if it drops (e.g. at-spi restarted).
var a11yConn struct {
	mu   sync.Mutex
	conn *dbus.Conn
}

func getA11yConn() (*dbus.Conn, error) {
	a11yConn.mu.Lock()
	defer a11yConn.mu.Unlock()
	if a11yConn.conn != nil && a11yConn.conn.Connected() {
		return a11yConn.conn, nil
	}
	session, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("session bus: %w", err)
	}
	var addr string
	if err := session.Object("org.a11y.Bus", "/org/a11y/bus").
		Call("org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		return nil, fmt.Errorf("a11y bus address: %w", err)
	}
	conn, err := dbus.Connect(addr)
	if err != nil {
		return nil, fmt.Errorf("a11y bus connect: %w", err)
	}
	a11yConn.conn = conn
	return conn, nil
}

// focusTracker remembers the accessible that most recently GAINED keyboard
// focus, learned from live AT-SPI focus signals. This is the element Ctrl+V
// will actually hit — unlike Collection.GetMatches(FOCUSED), which in VS Code
// returns several stale-focused fields at once (the open editor's full-text
// mirror AND the chat input), with no reliable way to tell which is live.
// The signal fires exactly when focus moves, so the last one is the truth.
var focusTracker struct {
	mu  sync.Mutex
	ref ref
	at  time.Time
	on  bool
}

func (t *atspiClient) trackedFocus() (ref, bool) {
	focusTracker.mu.Lock()
	defer focusTracker.mu.Unlock()
	if focusTracker.ref.Path == "" || time.Since(focusTracker.at) > 10*time.Minute {
		return ref{}, false
	}
	return focusTracker.ref, true
}

// ensureFocusTracker starts the background focus-signal listener once.
func ensureFocusTracker() {
	focusTracker.mu.Lock()
	already := focusTracker.on
	focusTracker.on = true
	focusTracker.mu.Unlock()
	if !already {
		go focusTrackerLoop()
	}
}

// focusTrackerLoop subscribes to AT-SPI focus signals and records the element
// that last gained focus. Reconnects on error.
func focusTrackerLoop() {
	for {
		if err := focusTrackerRun(); err != nil {
			time.Sleep(3 * time.Second)
		}
	}
}

func focusTrackerRun() error {
	session, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	var addr string
	if err := session.Object("org.a11y.Bus", "/org/a11y/bus").
		Call("org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		return err
	}
	conn, err := dbus.Connect(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Only focus-state changes (arg0 = "focused").
	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.a11y.atspi.Event.Object',member='StateChanged',arg0='focused'")
	ch := make(chan *dbus.Signal, 64)
	conn.Signal(ch)
	for sig := range ch {
		if len(sig.Body) < 2 {
			continue
		}
		state, _ := sig.Body[0].(string)
		gained, _ := sig.Body[1].(int32)
		if state == "focused" && gained == 1 {
			focusTracker.mu.Lock()
			focusTracker.ref = ref{Name: sig.Sender, Path: sig.Path}
			focusTracker.at = time.Now()
			focusTracker.mu.Unlock()
		}
	}
	return fmt.Errorf("focus signal channel closed")
}

// Capture reads the focused UI element via AT-SPI and returns the
// insertion-point surroundings. Never blocks past captureTimeout;
// every failure mode degrades to HasContext=false with a diagnostic
// Status, so the caller's Apply becomes a no-op.
func Capture() Context {
	ensureFocusTracker()
	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()

	pid, rect, err := activeWindow(ctx)
	if err != nil {
		return Context{Status: "no-active-window: " + err.Error()}
	}
	conn, err := getA11yConn()
	if err != nil {
		return Context{Status: "no-a11y-bus: " + err.Error()}
	}
	c := &atspiClient{ctx: ctx, conn: conn}
	return c.capture(pid, rect)
}

// winRect is the active window's screen rectangle (px). w==0 means unknown.
type winRect struct{ x, y, w, h int }

func (r winRect) known() bool { return r.w > 0 && r.h > 0 }

// contains reports whether the screen point (px,py) is inside the rect.
func (r winRect) contains(px, py int) bool {
	return px >= r.x && px < r.x+r.w && py >= r.y && py < r.y+r.h
}

// activeWindow asks X11 (via xdotool, already a paster dependency) for the
// focused toplevel window's owning PID and its screen geometry. The geometry
// lets capture() pick the focused field that actually lives in the window the
// user is in — Electron (VS Code) doesn't set STATE_ACTIVE on its frames, and
// with several windows/panels open the app-wide focus search otherwise grabs
// a stale input from a different window.
func activeWindow(ctx context.Context) (uint32, winRect, error) {
	out, err := exec.CommandContext(ctx, "xdotool", "getactivewindow",
		"getwindowpid", "getwindowgeometry", "--shell").Output()
	if err != nil {
		return 0, winRect{}, err
	}
	var pid uint32
	var r winRect
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "X="):
			r.x, _ = strconv.Atoi(line[2:])
		case strings.HasPrefix(line, "Y="):
			r.y, _ = strconv.Atoi(line[2:])
		case strings.HasPrefix(line, "WIDTH="):
			r.w, _ = strconv.Atoi(line[6:])
		case strings.HasPrefix(line, "HEIGHT="):
			r.h, _ = strconv.Atoi(line[7:])
		case strings.HasPrefix(line, "WINDOW="):
			// ignore
		default:
			// The bare getwindowpid line (first numeric line).
			if p, perr := strconv.ParseUint(line, 10, 32); perr == nil && pid == 0 {
				pid = uint32(p)
			}
		}
	}
	if pid == 0 {
		return 0, winRect{}, fmt.Errorf("no pid in xdotool output")
	}
	return pid, r, nil
}

type atspiClient struct {
	ctx  context.Context
	conn *dbus.Conn
}

func (c *atspiClient) capture(pid uint32, rect winRect) Context {
	apps, err := c.appsForPID(pid)
	if err != nil {
		return Context{Status: "desktop-scan: " + err.Error()}
	}
	if len(apps) == 0 {
		return Context{Status: fmt.Sprintf("no-a11y-app(pid=%d)", pid)}
	}

	// Collect every focused element across the app(s). VS Code keeps a
	// STATE_FOCUSED input in EACH open window/panel, so this can return
	// several — the geometry pass below picks the one in the active window.
	var matches []ref
	for _, app := range apps {
		m, err := c.focusedIn(app)
		if err != nil {
			continue // no Collection support on this root — skip
		}
		matches = append(matches, m...)
	}
	if len(matches) == 0 {
		return Context{Status: "no-focused-element"}
	}

	// Best signal first: the element that LAST gained focus (from live AT-SPI
	// focus signals) is the one keystrokes — and our paste — actually hit. Use
	// it when it belongs to the active window's process and sits inside the
	// active window's rect (guards against a stale focus in another window of
	// the same process). This is what resolves VS Code marking both the editor
	// mirror and the chat input "focused" at once.
	if f, ok := c.trackedFocus(); ok {
		for _, app := range apps {
			if app.Name != f.Name {
				continue
			}
			inRect := true
			if rect.known() {
				if x, y, w, h, ok := c.extents(f); ok && h > 0 {
					inRect = rect.contains(x+w/2, y+h/2)
				}
			}
			if inRect {
				// The tracked element IS the paste target. Its reading wins
				// outright — even when it's unreadable/empty (opaque chat
				// placeholder → HasContext=false → passthrough, i.e. keep the
				// phrase's own capital + terminator). Falling through to other
				// "focused" fields here is exactly what made an empty chat read
				// the editor's mirror and mangle the insert.
				cx, _ := c.readContext(f)
				return cx
			}
			break
		}
	}

	// Fallback (no usable tracked focus): order candidates so the best is first:
	//   1. inside the active window's screen rect (the field the user is in),
	//   2. editable,
	//   3. fewest characters.
	// (1) drops stale focus from OTHER windows (Electron doesn't mark its
	// active frame, so geometry is the only signal). (3) breaks the tie WITHIN
	// the active window: VS Code keeps BOTH the chat input (a small box) and
	// the open editor's full-document text mirror (thousands of chars) marked
	// focused — the user dictates into the small input, so prefer it. When
	// geometry is unknown we fall back to editable-then-fewest-chars.
	type cand struct {
		ref      ref
		inRect   bool
		editable bool
		chars    int
	}
	cands := make([]cand, 0, len(matches))
	for _, m := range matches {
		in := false
		if rect.known() {
			// Only h>0 is required, NOT w>0: VS Code exposes the LIVE editor
			// line as a zero-WIDTH element (a caret-like sliver), while its
			// stale full-document mirror keeps a wide rect. Requiring w>0 here
			// dropped the live field and let the mirror win — the empty-text-
			// file bug. The live field still has a line's worth of height.
			if x, y, w, h, ok := c.extents(m); ok && h > 0 {
				in = rect.contains(x+w/2, y+h/2)
			}
		}
		n, err := c.charCount(m)
		if err != nil {
			n = 1 << 30 // unreadable count → sort last
		}
		cands = append(cands, cand{ref: m, inRect: in, editable: c.isEditable(m), chars: n})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].inRect != cands[j].inRect {
			return cands[i].inRect // in-rect first
		}
		if cands[i].editable != cands[j].editable {
			return cands[i].editable // then editable first
		}
		return cands[i].chars < cands[j].chars // then the smaller field (the input, not the doc mirror)
	})

	lastStatus := "no-readable-focus"
	for _, cn := range cands {
		ctx, status := c.readContext(cn.ref)
		if ctx.HasContext {
			return ctx
		}
		// An opaque rich editor (a VS Code / Electron webview chat
		// input) IS the field the user is in — it just hides its real text
		// behind an embed placeholder. The sort already put it first (in the
		// active window's rect, editable, fewest chars), so reaching it here
		// means it's the paste target: return passthrough (the phrase keeps
		// its own capital + terminator, i.e. a fresh start) and STOP. Falling
		// through to the next focused field is exactly what made an empty chat
		// read the open editor's full-document mirror and mangle the insert as
		// mid-sentence — the bug this whole geometry pass exists to prevent.
		if strings.HasPrefix(status, "opaque-editor") {
			return Context{Status: status}
		}
		if status != "" {
			lastStatus = status
		}
	}
	return Context{Status: lastStatus}
}

// extents returns the on-screen rectangle of an accessible via the Component
// interface (ATSPI_COORD_TYPE_SCREEN). ok=false when it has no Component.
func (c *atspiClient) extents(r ref) (x, y, w, h int, ok bool) {
	var ex struct{ X, Y, W, H int32 }
	if err := c.call(r, "org.a11y.atspi.Component.GetExtents", uint32(0)).Store(&ex); err != nil {
		return 0, 0, 0, 0, false
	}
	return int(ex.X), int(ex.Y), int(ex.W), int(ex.H), true
}

// appsForPID returns the a11y-bus application roots owned by pid.
// (One process may register more than one root — Telegram does.)
func (c *atspiClient) appsForPID(pid uint32) ([]ref, error) {
	var children []ref
	if err := c.call(ref{registryName, registryRoot}, ifaceAccessible+".GetChildren").Store(&children); err != nil {
		return nil, err
	}
	pidByOwner := map[string]uint32{}
	var apps []ref
	for _, ch := range children {
		owner, ok := pidByOwner[ch.Name]
		if !ok {
			var p uint32
			err := c.conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus").
				CallWithContext(c.ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, ch.Name).
				Store(&p)
			if err != nil {
				continue
			}
			pidByOwner[ch.Name] = p
			owner = p
		}
		if owner == pid {
			apps = append(apps, ch)
		}
	}
	return apps, nil
}

// focusedIn asks the app's own toolkit for its focused descendants via
// the Collection interface — one round-trip, no tree crawling. The
// match rule's wire shape is (aiia{ss}iaiiasib).
func (c *atspiClient) focusedIn(app ref) ([]ref, error) {
	type matchRule struct {
		States     []int32
		StateMatch int32
		Attributes map[string]string
		AttrMatch  int32
		Roles      []int32
		RoleMatch  int32
		Interfaces []string
		IfaceMatch int32
		Invert     bool
	}
	rule := matchRule{
		States:     []int32{1 << stateFocused, 0},
		StateMatch: matchAll,
		Attributes: map[string]string{},
		AttrMatch:  matchNone,
		Roles:      []int32{},
		RoleMatch:  matchNone,
		Interfaces: []string{},
		IfaceMatch: matchNone,
	}
	var out []ref
	err := c.call(app, ifaceCollection+".GetMatches", rule, sortCanonical, int32(8), true).Store(&out)
	return out, err
}

// readContext extracts the insertion-point Context from one focused
// element. Returns a non-empty status when the element is unusable
// (no Text, caret unknown, terminal).
func (c *atspiClient) readContext(m ref) (Context, string) {
	return c.readContextDepth(m, 0)
}

// readContextDepth reads insertion context from m. Some Electron rich inputs
// (the Codex panel, eXpress) expose the focused editable as a single-embed
// WRAPPER whose own caret is pinned at 0, while the real text and live caret
// live in an editable text child (a paragraph). depth bounds the descent into
// such children (see caretBearingChild).
func (c *atspiClient) readContextDepth(m ref, depth int) (Context, string) {
	role, err := c.roleName(m)
	if err != nil {
		return Context{}, "role: " + err.Error()
	}
	if role == "terminal" {
		// Screen-buffer carets are unverified territory; transforming a
		// shell command line on a guess is worse than no transform.
		return Context{}, "terminal-skip"
	}
	if !c.hasText(m) {
		return Context{}, "no-text-iface(" + role + ")"
	}
	count, err := c.charCount(m)
	if err != nil {
		return Context{}, "char-count: " + err.Error()
	}
	if c.opaqueRichEditor(m, count) {
		// The field's real text never reaches the a11y tree — see
		// opaqueRichEditor. Transforming against the stale placeholder
		// labels it DOES expose mangles every insert, so bail to the
		// untouched-passthrough behaviour.
		return Context{}, "opaque-editor(" + role + ")"
	}
	// Embed-wrapper whose real text + live caret live in an editable child
	// (Codex / eXpress): read the context from that child, not the wrapper
	// (whose caret is pinned at 0 and would mis-read every insert as a fresh
	// start). Proven live 2026-06-23: wrapper /4 text='￼' caret=0, child /7
	// [paragraph] = the typed text with caret at its end.
	if depth < 6 {
		if child, ok := c.caretBearingChild(m, count); ok {
			return c.readContextDepth(child, depth+1)
		}
	}
	caret, err := c.caretOffset(m)
	if err != nil {
		return Context{}, "caret: " + err.Error()
	}

	left, right := caret, caret
	if s, e, ok := c.selection(m); ok {
		if s == 0 && e >= count && count > 0 {
			// The whole field is selected: the paste replaces
			// everything, i.e. lands in an empty field.
			return Context{
				HasContext: true, AtStart: true,
				RightKnown: true, AtEnd: true,
				Status: "ok:replace-all(" + role + ")",
			}, ""
		}
		left, right = s, e
	} else if caret < 0 {
		return Context{}, "caret-unknown(" + role + ")"
	}
	if left < 0 || left > count || right < 0 || right > count {
		return Context{}, fmt.Sprintf("bounds(%s: %d/%d of %d)", role, left, right, count)
	}

	preceding, spaceBefore, atStart, err := c.precedingAt(m, left)
	if err != nil {
		return Context{}, "left-scan: " + err.Error()
	}
	out := Context{
		HasContext:  true,
		AtStart:     atStart,
		Preceding:   preceding,
		SpaceBefore: spaceBefore,
		Status:      "ok:" + role,
	}
	// The right side is a nice-to-have: on failure keep the (already
	// useful) left half and let the tail rules stay conservative.
	if following, atEnd, err := c.followingAt(m, right); err == nil {
		out.RightKnown = true
		out.AtEnd = atEnd
		out.Following = following
	}
	return out, ""
}

// opaqueRichEditor reports whether the focused editable element hides
// its real text behind embed placeholders that resolve only to
// read-only content — the signature of a custom rich editor (e.g. the
// VS Code / Electron chat inputs) that exposes a single U+FFFC and a
// pinned caret, while the typed text never reaches the accessibility
// tree at all. Drilling those placeholders yields stale label strings
// ("Queue another message…", "ctrl esc to focus…"), so any transform
// built on them is garbage. Verified live 2026-06-10: entry [EDIT]
// text='￼' caret=(1,1) → section [ro] = placeholder.
//
// Conservative by design: a field with any real readable text, or one
// whose embeds reach an editable text child we could actually read, is
// NOT opaque and is handled normally.
func (c *atspiClient) opaqueRichEditor(node ref, count int) bool {
	if count <= 0 || count > 64 {
		// Empty field (handled elsewhere) or enough real text that it
		// can't be a bare placeholder shell.
		return false
	}
	txt, err := c.getText(node, 0, count)
	if err != nil {
		return false
	}
	embeds := 0
	for _, r := range txt {
		switch {
		case r == embedChar:
			embeds++
		case !unicode.IsSpace(r):
			return false // real, readable text present — trust the field
		}
	}
	if embeds == 0 {
		return false
	}
	// Content is entirely embed placeholders: trustworthy only if one
	// resolves to an editable text child we could actually read.
	for i := 0; i < embeds && i < 8; i++ {
		ch, err := c.childAt(node, i)
		if err != nil {
			continue
		}
		if c.hasText(ch) && c.isEditable(ch) {
			return false
		}
	}
	return true
}

// caretBearingChild returns the editable text child that actually holds the
// live caret, when `node` is an embed-WRAPPER (its content is entirely U+FFFC
// placeholders) but a child paragraph carries the real text. This is the
// signature of the Codex panel / eXpress message box: the wrapper's own caret
// is pinned at 0, so reading it directly mis-reports every insert as a fresh
// start — the child paragraph is where the text and caret really are.
//
// Returns false for normal fields (real text in the node itself) and for
// empty wrappers (no child with content), so the empty-field / fresh-start
// and opaque-passthrough behaviours are untouched.
func (c *atspiClient) caretBearingChild(node ref, count int) (ref, bool) {
	if count <= 0 || count > 64 {
		return ref{}, false
	}
	txt, err := c.getText(node, 0, count)
	if err != nil {
		return ref{}, false
	}
	embeds := 0
	for _, r := range txt {
		switch {
		case r == embedChar:
			embeds++
		case !unicode.IsSpace(r):
			return ref{}, false // real text in the node itself — read it directly
		}
	}
	if embeds == 0 {
		return ref{}, false
	}
	// Pick the editable text child with content that reports the furthest
	// caret offset — that's the paragraph the user is editing (inactive
	// paragraphs report caret -1 / 0).
	var best ref
	bestCaret := -1
	for i := 0; i < embeds && i < 16; i++ {
		ch, err := c.childAt(node, i)
		if err != nil {
			continue
		}
		if !c.hasText(ch) || !c.isEditable(ch) {
			continue
		}
		if n, _ := c.charCount(ch); n <= 0 {
			continue
		}
		if cr, _ := c.caretOffset(ch); cr > bestCaret {
			bestCaret, best = cr, ch
		}
	}
	if bestCaret >= 0 {
		return best, true
	}
	return ref{}, false
}

// frame remembers where an embed descent left the parent: embedPos is
// the placeholder character's offset in the parent's text.
type frame struct {
	node     ref
	embedPos int
}

// precedingAt finds the first non-blank character left of offset off,
// drilling into embedded children (Chromium paragraphs/inline spans).
// Returns atStart=true when only blanks (or nothing) precede the
// insertion point all the way to the field start.
func (c *atspiClient) precedingAt(node ref, off int) (preceding rune, spaceBefore bool, atStart bool, err error) {
	var stack []frame
	depth := 0
	for {
		for off > 0 {
			lo := off - 64
			if lo < 0 {
				lo = 0
			}
			chunk, terr := c.getText(node, lo, off)
			if terr != nil {
				return 0, false, false, terr
			}
			runes := []rune(chunk)
			if len(runes) == 0 {
				return 0, false, false, fmt.Errorf("empty text chunk [%d:%d)", lo, off)
			}
			descended := false
			for i := len(runes) - 1; i >= 0; i-- {
				ch := runes[i]
				off--
				if ch == '\n' {
					return '\n', spaceBefore, false, nil
				}
				if ch == embedChar {
					child, kind, cerr := c.resolveEmbed(node, off)
					if cerr != nil {
						return 0, false, false, cerr
					}
					switch kind {
					case embedOpaque:
						// Image/control — no safe guess; Apply leaves
						// the text alone on this rune.
						return embedChar, spaceBefore, false, nil
					case embedEmptyBlock:
						// An empty paragraph is an empty line above.
						return '\n', spaceBefore, false, nil
					case embedTextChild:
						if !c.isEditable(child) {
							// Non-editable embedded text is a placeholder / hint
							// ("Enter message"), not typed content. Skip it so a
							// field holding only a placeholder reads as empty → a
							// fresh start, per the general rule (no real letters to
							// the left ⇒ treat as empty).
							continue
						}
						if depth++; depth > maxDescend {
							return embedChar, spaceBefore, false, nil
						}
						cc, cerr := c.charCount(child)
						if cerr != nil {
							return 0, false, false, cerr
						}
						pos := cc
						if ca, cerr := c.caretOffset(child); cerr == nil && ca > 0 && ca <= cc {
							pos = ca
						}
						stack = append(stack, frame{node, off})
						node, off = child, pos
						descended = true
					}
				}
				if descended {
					break
				}
				if unicode.IsSpace(ch) {
					spaceBefore = true
					continue
				}
				if !unicode.IsGraphic(ch) {
					// Invisible control / format rune (U+FEFF BOM, zero-width
					// spaces) — Electron inputs pad an empty field with these.
					// They are not a real preceding character, so skip. The
					// general rule: if the whole left context collapses to
					// invisibles + spaces + newlines with no letters, the field
					// is empty ⇒ fresh start (capitalised).
					continue
				}
				return ch, spaceBefore, false, nil
			}
			if descended {
				break // restart the outer loop inside the child
			}
		}
		if off > 0 {
			continue // descended mid-chunk; keep scanning in the child
		}
		// Start of this node's text.
		if len(stack) == 0 {
			return 0, spaceBefore, true, nil
		}
		fr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if fr.embedPos == 0 {
			// First block of its parent — bubble up and keep looking.
			node, off = fr.node, 0
			continue
		}
		// A previous sibling block exists: visually a line break.
		return '\n', spaceBefore, false, nil
	}
}

// followingAt reports the character immediately right of offset off
// (NOT blank-skipped — the tail rules need the raw neighbour), or
// atEnd when nothing follows. Embedded children to the right read as
// line breaks: a following block lives on the next line.
func (c *atspiClient) followingAt(node ref, off int) (following rune, atEnd bool, err error) {
	var stack []frame
	cur, pos := node, off
	for range [maxDescend + 1]struct{}{} {
		cc, cerr := c.charCount(cur)
		if cerr != nil {
			return 0, false, cerr
		}
		if pos < cc {
			chunk, terr := c.getText(cur, pos, pos+1)
			if terr != nil {
				return 0, false, terr
			}
			runes := []rune(chunk)
			if len(runes) == 0 {
				return 0, false, fmt.Errorf("empty text chunk [%d:%d)", pos, pos+1)
			}
			if runes[0] == embedChar {
				return '\n', false, nil
			}
			return runes[0], false, nil
		}
		if len(stack) == 0 {
			return 0, true, nil
		}
		fr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		cur, pos = fr.node, fr.embedPos+1
	}
	return 0, true, nil
}

type embedKind int

const (
	embedOpaque embedKind = iota
	embedEmptyBlock
	embedTextChild
)

// resolveEmbed maps the placeholder at text offset pos to the child
// accessible it stands for and classifies it: a text-bearing child to
// drill into, an empty block (= empty line), or an opaque object.
func (c *atspiClient) resolveEmbed(node ref, pos int) (ref, embedKind, error) {
	idx, err := c.embedIndex(node, pos)
	if err != nil {
		return ref{}, embedOpaque, err
	}
	child, err := c.childAt(node, idx)
	if err != nil {
		return ref{}, embedOpaque, err
	}
	if !c.hasText(child) {
		return child, embedOpaque, nil
	}
	cc, err := c.charCount(child)
	if err != nil || cc == 0 {
		return child, embedEmptyBlock, nil
	}
	return child, embedTextChild, nil
}

// embedIndex counts placeholder characters strictly before pos — that
// count is the child index the placeholder at pos refers to.
func (c *atspiClient) embedIndex(node ref, pos int) (int, error) {
	if pos > maxEmbedScan {
		return 0, fmt.Errorf("embed scan too large (%d)", pos)
	}
	count := 0
	for lo := 0; lo < pos; lo += 1024 {
		hi := lo + 1024
		if hi > pos {
			hi = pos
		}
		chunk, err := c.getText(node, lo, hi)
		if err != nil {
			return 0, err
		}
		count += strings.Count(chunk, string(embedChar))
	}
	return count, nil
}

// --- thin D-Bus accessors -------------------------------------------------

func (c *atspiClient) call(r ref, method string, args ...any) *dbus.Call {
	return c.conn.Object(r.Name, r.Path).CallWithContext(c.ctx, method, 0, args...)
}

func (c *atspiClient) textProp(r ref, name string) (int, error) {
	var v dbus.Variant
	if err := c.call(r, "org.freedesktop.DBus.Properties.Get", ifaceText, name).Store(&v); err != nil {
		return 0, err
	}
	n, ok := v.Value().(int32)
	if !ok {
		return 0, fmt.Errorf("%s: unexpected type %T", name, v.Value())
	}
	return int(n), nil
}

func (c *atspiClient) charCount(r ref) (int, error) {
	return c.textProp(r, "CharacterCount")
}

func (c *atspiClient) caretOffset(r ref) (int, error) {
	return c.textProp(r, "CaretOffset")
}

func (c *atspiClient) getText(r ref, start, end int) (string, error) {
	var s string
	err := c.call(r, ifaceText+".GetText", int32(start), int32(end)).Store(&s)
	return s, err
}

// selection returns the first selection range, ok=false when there is
// none (or it is collapsed).
func (c *atspiClient) selection(r ref) (start, end int, ok bool) {
	var n int32
	if err := c.call(r, ifaceText+".GetNSelections").Store(&n); err != nil || n <= 0 {
		return 0, 0, false
	}
	var s, e int32
	if err := c.call(r, ifaceText+".GetSelection", int32(0)).Store(&s, &e); err != nil || s == e {
		return 0, 0, false
	}
	if s > e {
		s, e = e, s
	}
	return int(s), int(e), true
}

func (c *atspiClient) roleName(r ref) (string, error) {
	var s string
	err := c.call(r, ifaceAccessible+".GetRoleName").Store(&s)
	return s, err
}

func (c *atspiClient) hasText(r ref) bool {
	var ifaces []string
	if err := c.call(r, ifaceAccessible+".GetInterfaces").Store(&ifaces); err != nil {
		return false
	}
	for _, s := range ifaces {
		if s == ifaceText {
			return true
		}
	}
	return false
}

func (c *atspiClient) isEditable(r ref) bool {
	return c.hasState(r, stateEditable)
}

// hasState reports whether the accessible has the given ATSPI_STATE bit set
// in the first word of its state set.
func (c *atspiClient) hasState(r ref, bit uint) bool {
	var words []uint32
	if err := c.call(r, ifaceAccessible+".GetState").Store(&words); err != nil || len(words) == 0 {
		return false
	}
	return words[0]&(1<<bit) != 0
}

func (c *atspiClient) childAt(r ref, i int) (ref, error) {
	var ch ref
	err := c.call(r, ifaceAccessible+".GetChildAtIndex", int32(i)).Store(&ch)
	return ch, err
}
