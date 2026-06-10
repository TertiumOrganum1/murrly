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

	// stateFocused / stateEditable are ATSPI_STATE_* enum values; the
	// match rule and the editable check use them as bit positions in
	// the state set's first word.
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

// Capture reads the focused UI element via AT-SPI and returns the
// insertion-point surroundings. Never blocks past captureTimeout;
// every failure mode degrades to HasContext=false with a diagnostic
// Status, so the caller's Apply becomes a no-op.
func Capture() Context {
	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()

	pid, err := activeWindowPID(ctx)
	if err != nil {
		return Context{Status: "no-active-window: " + err.Error()}
	}
	conn, err := getA11yConn()
	if err != nil {
		return Context{Status: "no-a11y-bus: " + err.Error()}
	}
	c := &atspiClient{ctx: ctx, conn: conn}
	return c.capture(pid)
}

// activeWindowPID asks X11 (via xdotool, already a paster dependency)
// which process owns the focused toplevel window.
func activeWindowPID(ctx context.Context) (uint32, error) {
	out, err := exec.CommandContext(ctx, "xdotool", "getactivewindow", "getwindowpid").Output()
	if err != nil {
		return 0, err
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(pid), nil
}

type atspiClient struct {
	ctx  context.Context
	conn *dbus.Conn
}

func (c *atspiClient) capture(pid uint32) Context {
	apps, err := c.appsForPID(pid)
	if err != nil {
		return Context{Status: "desktop-scan: " + err.Error()}
	}
	if len(apps) == 0 {
		return Context{Status: fmt.Sprintf("no-a11y-app(pid=%d)", pid)}
	}

	var matches []ref
	for _, app := range apps {
		m, err := c.focusedIn(app)
		if err != nil {
			continue // app without Collection support — skip
		}
		matches = append(matches, m...)
	}
	if len(matches) == 0 {
		return Context{Status: "no-focused-element"}
	}

	// Editable fields first: the duplicate/companion matches (e.g. the
	// containing document) read worse than the input itself.
	ordered := make([]ref, 0, len(matches))
	var rest []ref
	for _, m := range matches {
		if c.isEditable(m) {
			ordered = append(ordered, m)
		} else {
			rest = append(rest, m)
		}
	}
	ordered = append(ordered, rest...)

	lastStatus := "no-readable-focus"
	for _, m := range ordered {
		ctx, status := c.readContext(m)
		if ctx.HasContext {
			return ctx
		}
		if status != "" {
			lastStatus = status
		}
	}
	return Context{Status: lastStatus}
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
	var words []uint32
	if err := c.call(r, ifaceAccessible+".GetState").Store(&words); err != nil || len(words) == 0 {
		return false
	}
	return words[0]&(1<<stateEditable) != 0
}

func (c *atspiClient) childAt(r ref, i int) (ref, error) {
	var ch ref
	err := c.call(r, ifaceAccessible+".GetChildAtIndex", int32(i)).Store(&ch)
	return ch, err
}
