// Package uicontext reads the cursor / surrounding text in whichever
// app currently has the input focus, so we can adapt the recognised
// transcription to the insertion point (add a leading space, lower-
// case the first letter when slipping into the middle of a phrase,
// drop the terminal punctuation mid-sentence, etc).
//
// On macOS this uses the Accessibility (AX) API — the same permission
// the paster already requires for CGEventPost-based Cmd+V. On Linux it
// talks AT-SPI2 (the accessibility D-Bus that screen readers use); see
// atspi_linux.go for the per-toolkit enablement matrix. On other
// platforms Capture returns HasContext=false so Apply becomes a no-op
// and the original text passes through unchanged.
package uicontext

// Context describes the state at the insertion point of the focused
// UI element — what surrounds the spot where the paste will land.
//
// HasContext is the gatekeeper: false means we couldn't read the
// element (no focus, app refused, sandboxed field, terminal). Apply
// will then leave the text untouched.
//
// When a selection exists the paste replaces it, so Capture normalises
// the fields against the selection's edges instead of the caret: a
// selection spanning the whole field reads as AtStart+AtEnd (the paste
// effectively lands in an empty field), a partial one contributes its
// left edge to Preceding/SpaceBefore and its right edge to
// Following/AtEnd.
type Context struct {
	HasContext bool
	// ForceMid forces the mid-sentence transform unconditionally,
	// bypassing all field reading and heuristics: lower-case the first
	// letter, prepend a single leading space, strip the phrase's own
	// terminal punctuation. Set by the Shift+F12 hotkey when the user
	// KNOWS they're slipping text into the middle of a sentence and
	// doesn't want Capture to guess. Takes priority over every other
	// field below.
	ForceMid bool
	// AtStart is true when nothing (not even blanks) precedes the
	// insertion point — empty field, caret at offset 0, or the whole
	// content selected for replacement.
	AtStart bool
	// Preceding is the first non-blank character to the left of the
	// insertion point. '\n' when a line/paragraph boundary is the
	// nearest thing on the left. Valid only when HasContext is true
	// and AtStart is false.
	//
	// The macOS capture fills this with the RAW character immediately
	// left of the caret (it cannot skip blanks), so a plain space is a
	// legal value there; Apply treats it as "ambiguous, do nothing" —
	// the historical darwin behaviour.
	Preceding rune
	// SpaceBefore is true when one or more blanks sit immediately left
	// of the insertion point (Preceding is then the first character
	// before that run). A leading space must not be added again.
	SpaceBefore bool
	// RightKnown reports whether the right side of the insertion point
	// was read at all. False on macOS (the AX capture only looks left),
	// in which case tail handling stays conservative.
	RightKnown bool
	// AtEnd is true when nothing follows the insertion point. Valid
	// only when RightKnown.
	AtEnd bool
	// Following is the character immediately right of the insertion
	// point (0 when AtEnd). Deliberately NOT blank-skipped: the tail
	// rules need to know whether a space is already present there.
	Following rune
	// Status is a diagnostic label describing which capture step
	// succeeded last — useful in the log when a transform did nothing
	// and we want to know why (no focus? terminal? a11y bridge off?).
	Status string
}
