// Command picker is Murrly's multi-inference variant chooser. It's a
// standalone Fyne GUI spawned by the main murrly process (Ctrl+F11):
// the variants come in on stdin (NUL-separated, since each may span
// several lines), it shows them as flat hover-highlighting cards, and
// prints the 0-based index of the clicked card to stdout (exit 0).
// Cancel (Esc / window close) exits non-zero with no output.
//
// It lives in its own binary because fyne.io/systray (the tray icon in
// the main process) and a Fyne GUI both demand the main OS thread —
// they can't coexist in one process. Spawning keeps them isolated.
package main

import (
	_ "embed"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// appID is both the Fyne unique ID and (loosely) the window identity; the
// parent finds the window by the spawned PID, so this is just metadata.
const appID = "murrly-picker"

// windowWidth is the fixed logical width of the picker. Height is grown
// to exactly fit the cards (see newPickerWindow), so the window never
// has empty space below the last option.
const windowWidth = 820

// gutterSep splits each option into its fixed-width left gutter (marks +
// glyph on line 1, scores on line 2) and the reply text. The parent
// (cmd/murrly) joins them with this; the picker reserves a uniform gutter
// width so every card's text starts at the same x.
const gutterSep = "\x1f"

// catIcon is Murrly's cat-head mark, shown as the window/taskbar icon so
// the picker reads as part of Murrly rather than a stray Fyne window.
//
//go:embed icon.png
var catIcon []byte

// picked flips true the instant the user clicks a card, so the focus-dismiss
// watcher won't mistake the deliberate focus hand-back (to the editor) for a
// click-away cancel.
var picked atomic.Bool

func main() {
	// Record the editor that spawned us before we create/show any window, so
	// the pick handler can hand focus back to it (Windows only; no-op elsewhere).
	notePrevForeground()

	options, err := readOptions(os.Stdin)
	if err != nil || len(options) == 0 {
		os.Exit(1)
	}

	// Fyne's X11 auto-detection often falls back to 1.0 on this desktop
	// (Cinnamon HiDPI), rendering text unreadably small. Honour the scale
	// the parent passes in FYNE_SCALE; if it's unset (e.g. running the
	// binary by hand), derive it from the X server's Xft.dpi.
	ensureScale()

	a := app.NewWithID(appID)
	a.SetIcon(fyne.NewStaticResource("murrly", catIcon))
	// Force the dark variant: card colors below are explicit dark tones,
	// and a system-driven light theme would wash them out (the bug where
	// every card looked white). Fixing the variant keeps the palette
	// predictable regardless of the desktop's GTK theme.
	a.Settings().SetTheme(darkTheme{})

	w := newPickerWindow(a, options)
	// Drop the taskbar entry and centre on the mouse's monitor once the
	// window maps (runs concurrently with ShowAndRun's blocking GL loop).
	go arrangeWindow()
	w.ShowAndRun()
	// ShowAndRun returns after the window closes. The chosen index (if
	// any) was already printed by the card's tap handler before Quit.
}

func readOptions(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	s := strings.TrimRight(string(data), "\x00")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\x00"), nil
}

// ensureScale sets FYNE_SCALE from the X server DPI when the parent
// didn't already set it. Best-effort: on any failure we leave Fyne's own
// detection in place.
func ensureScale() {
	if os.Getenv("FYNE_SCALE") != "" {
		return
	}
	if s := scaleFromXDPI(); s > 0 {
		os.Setenv("FYNE_SCALE", strconv.FormatFloat(s, 'f', 2, 64))
	}
}

// scaleFromXDPI reads Xft.dpi from `xrdb -query` and converts it to a
// Fyne scale (DPI / 96, the conventional 1.0× baseline). Returns 0 when
// the value is missing or unreasonable.
func scaleFromXDPI() float64 {
	out, err := exec.Command("xrdb", "-query").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		v, ok := strings.CutPrefix(line, "Xft.dpi:")
		if !ok {
			continue
		}
		dpi, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || dpi <= 0 {
			return 0
		}
		scale := float64(dpi) / 96.0
		if scale < 1 {
			scale = 1
		}
		if scale > 3 {
			scale = 3
		}
		return scale
	}
	return 0
}

func newPickerWindow(a fyne.App, options []string) fyne.Window {
	var w fyne.Window
	if drv, ok := a.Driver().(desktop.Driver); ok {
		w = drv.CreateSplashWindow() // borderless — an overlay, not a titled dialog
	} else {
		w = a.NewWindow("Murrly")
	}
	w.SetTitle("Murrly") // avoids the default "Fyne Application" label
	w.SetIcon(fyne.NewStaticResource("murrly", catIcon))

	// Wrap each variant to up to maxCardLines lines so a long variant
	// shows real context instead of a single ellipsized line. availWidth
	// is a deliberately conservative text width (window minus paddings),
	// so the wrapped lines always fit the card without horizontal
	// overflow. Heights stay deterministic — the label gets the text
	// pre-broken with newlines and never re-wraps.
	textSize := theme.TextSize()
	style := fyne.TextStyle{}

	// Each option is "gutter\x1ftext". The gutter (marks + glyph on line 1,
	// scores on line 2) gets a uniform width across all cards, so every card's
	// reply text begins at the same x. Width = the widest gutter line + slack.
	type parsedOpt struct{ gutter, text string }
	parsed := make([]parsedOpt, len(options))
	var gutterWidth float32
	for i, o := range options {
		g, t, found := strings.Cut(o, gutterSep)
		if !found {
			g, t = "", o
		}
		parsed[i] = parsedOpt{gutter: g, text: t}
		for _, line := range strings.Split(g, "\n") {
			if wdt := fyne.MeasureText(line, textSize, style).Width; wdt > gutterWidth {
				gutterWidth = wdt
			}
		}
	}
	gutterWidth += 12 // breathing room so the gutter never abuts the text

	// Text column = window minus outer/card paddings minus the gutter.
	availWidth := float32(windowWidth) - 60 - gutterWidth

	cards := make([]fyne.CanvasObject, 0, len(parsed))
	for i, p := range parsed {
		// Wrap to ALL lines (not truncated): the card shows a maxCardLines
		// window and the mouse wheel scrolls the rest when the card is hovered.
		lines := wrapWords(oneLine(p.text), availWidth, textSize, style)
		cards = append(cards, newCard(i, p.gutter, gutterWidth, lines, maxCardLines, func(idx int) {
			picked.Store(true)
			fmt.Println(idx)
			restorePrevForeground() // give focus back to the editor before we exit, so the parent's paste lands
			w.Close()
		}))
	}

	content := container.NewVBox(cards...)
	root := container.NewPadded(content)
	w.SetContent(root)

	// Size to content: fixed width, height just tall enough for every
	// card. MinSize is deterministic because each card is a single
	// (ellipsized) line, so there's no leftover space below.
	min := root.MinSize()
	width := float32(windowWidth)
	if min.Width > width {
		width = min.Width
	}
	w.Resize(fyne.NewSize(width, min.Height))

	// Esc cancels without a selection.
	if deskCanvas, ok := w.Canvas().(desktop.Canvas); ok {
		deskCanvas.SetOnKeyDown(func(e *fyne.KeyEvent) {
			if e.Name == fyne.KeyEscape {
				os.Exit(1)
			}
		})
	}
	return w
}

// oneLine collapses all runs of whitespace (including newlines) to single
// spaces, giving fitLines a clean stream of words to re-wrap.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// maxCardLines caps how many wrapped lines a variant card shows. Whisper
// transcribes in ~30 s chunks and can go wrong mid-utterance, so several
// lines of context (not just the head) matter when judging a variant.
const maxCardLines = 5

// fitLines greedily word-wraps text into lines no wider than maxWidth and
// returns at most maxLines of them joined by "\n"; if the text is longer,
// the last shown line ends with an ellipsis. The label renders this with
// wrapping OFF, so what we compute here is exactly what's drawn — keeping
// card heights deterministic.
func fitLines(text string, maxWidth, textSize float32, style fyne.TextStyle, maxLines int) string {
	lines := wrapWords(text, maxWidth, textSize, style)
	if len(lines) == 0 {
		return ""
	}
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	lines = lines[:maxLines]
	lines[maxLines-1] = elide(lines[maxLines-1]+" …", maxWidth, textSize, style)
	return strings.Join(lines, "\n")
}

// wrapWords splits text into greedy word-wrapped lines bounded by maxWidth.
// A single word wider than maxWidth gets its own (overflowing) line —
// vanishingly rare for dictated speech.
func wrapWords(text string, maxWidth, textSize float32, style fyne.TextStyle) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if cur == "" || fyne.MeasureText(cand, textSize, style).Width <= maxWidth {
			cur = cand
			continue
		}
		lines = append(lines, cur)
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// elide drops whole trailing runes from s until it fits maxWidth, keeping
// a trailing ellipsis.
func elide(s string, maxWidth, textSize float32, style fyne.TextStyle) string {
	if fyne.MeasureText(s, textSize, style).Width <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		t := strings.TrimRight(string(runes), " ") + "…"
		if fyne.MeasureText(t, textSize, style).Width <= maxWidth {
			return t
		}
	}
	return "…"
}

// Explicit, opaque card tones for the forced dark theme. Hover is a clear
// step lighter than rest so the highlight is obvious and reverts cleanly
// on MouseOut (theme.ColorNameHover is a low-alpha overlay — wrong for an
// opaque fill, which is what made every card look white before).
var (
	cardRest  = color.NRGBA{R: 0x36, G: 0x3a, B: 0x42, A: 0xff} // clearly lifted off the ~#1c1c20 window bg
	cardHover = color.NRGBA{R: 0x5a, G: 0x63, B: 0x77, A: 0xff} // a big step lighter, so the highlight reads at a glance
)

// card is a flat, full-width clickable panel showing a variant's text.
// Background lightens on hover; a tap fires onTap(index) and closes. The card
// shows a `visible`-line window into the full wrapped text; when hovered, the
// mouse wheel scrolls that window so long variants can be read in full.
type card struct {
	widget.BaseWidget
	index       int
	gutter      string   // fixed-width left column: marks + glyph / scores
	gutterWidth float32  // uniform across cards, so text starts at one x
	lines       []string // full wrapped reply text, one entry per line
	visible     int      // lines shown at once (window height)
	offset      int      // index of the first visible line
	onTap       func(int)

	bg        *canvas.Rectangle
	textLabel *widget.Label
}

func newCard(index int, gutter string, gutterWidth float32, lines []string, visible int, onTap func(int)) *card {
	c := &card{index: index, gutter: gutter, gutterWidth: gutterWidth, lines: lines, visible: visible, onTap: onTap}
	c.ExtendBaseWidget(c)
	return c
}

// view returns the current window of lines: min(len, visible) lines starting
// at offset. Short variants get a short card (no empty padding); long ones get
// a fixed visible-line window the wheel scrolls. offset is clamped so the
// window is always full of real lines — height never jumps while scrolling.
func (c *card) view() string {
	n := c.visible
	if len(c.lines) < n {
		n = len(c.lines)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = c.lines[c.offset+i]
	}
	return strings.Join(out, "\n")
}

func (c *card) maxOffset() int {
	if len(c.lines) <= c.visible {
		return 0
	}
	return len(c.lines) - c.visible
}

func (c *card) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(cardRest)
	c.bg.CornerRadius = 6

	// Left gutter: marks/glyph + scores, in a cell forced to gutterWidth via a
	// transparent spacer so every card's text column starts at the same x. The
	// VBox top-anchors the (2-line) gutter against taller text cards.
	gutterLabel := widget.NewLabel(c.gutter)
	gutterLabel.Wrapping = fyne.TextWrapOff
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(c.gutterWidth, 0))
	gutterCell := container.NewStack(spacer, container.NewVBox(gutterLabel))

	c.textLabel = widget.NewLabel(c.view())
	c.textLabel.Wrapping = fyne.TextWrapOff // pre-wrapped to fixed-width lines

	row := container.NewBorder(nil, nil, gutterCell, nil, c.textLabel)
	padded := container.NewPadded(row)
	return &cardRenderer{card: c, objects: []fyne.CanvasObject{c.bg, padded}, content: padded}
}

func (c *card) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap(c.index)
	}
}

// Scrolled satisfies desktop.Scrollable: the wheel moves the visible window
// over the card's text (only when the text is taller than the window).
func (c *card) Scrolled(ev *fyne.ScrollEvent) {
	mo := c.maxOffset()
	if mo == 0 {
		return
	}
	if ev.Scrolled.DY < 0 {
		c.offset++
	} else {
		c.offset--
	}
	if c.offset < 0 {
		c.offset = 0
	}
	if c.offset > mo {
		c.offset = mo
	}
	if c.textLabel != nil {
		c.textLabel.SetText(c.view())
	}
}

func (c *card) MouseIn(*desktop.MouseEvent)    { c.setFill(cardHover) }
func (c *card) MouseMoved(*desktop.MouseEvent) {}
func (c *card) MouseOut()                      { c.setFill(cardRest) }

func (c *card) setFill(col color.Color) {
	if c.bg != nil {
		c.bg.FillColor = col
		c.bg.Refresh()
	}
}

type cardRenderer struct {
	card    *card
	objects []fyne.CanvasObject
	content fyne.CanvasObject
}

func (r *cardRenderer) Layout(size fyne.Size) {
	r.card.bg.Resize(size)
	r.content.Resize(size)
}
func (r *cardRenderer) MinSize() fyne.Size           { return r.content.MinSize() }
func (r *cardRenderer) Refresh()                     { r.card.bg.Refresh(); r.content.Refresh() }
func (r *cardRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *cardRenderer) Destroy()                     {}

// darkTheme wraps Fyne's default theme but pins the dark variant, so the
// window background and text stay legible against the explicit dark card
// colors no matter what the desktop reports.
type darkTheme struct{}

func (darkTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}
func (darkTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (darkTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (darkTheme) Size(n fyne.ThemeSizeName) float32       { return theme.DefaultTheme().Size(n) }
