// Package uicontext reads the cursor / surrounding text in whichever
// app currently has the input focus, so we can adapt the recognised
// transcription to the insertion point (add a leading space, lower-
// case the first letter when slipping into the middle of a phrase,
// drop the terminal period mid-sentence, etc).
//
// On macOS this uses the Accessibility (AX) API — the same permission
// the paster already requires for CGEventPost-based Cmd+V. On other
// platforms Capture returns HasContext=false so Apply becomes a no-op
// and the original text passes through unchanged.
package uicontext

// Context describes the state at the insertion point — what's
// immediately to the left of the cursor in the focused UI element.
//
// HasContext is the gatekeeper: false means we couldn't read the
// element (no focus, app refused AX, sandboxed field). Apply will
// then leave the text untouched.
type Context struct {
	HasContext bool
	// AtStart is true when the cursor sits at position 0 of the
	// focused element — i.e. there's nothing to the left.
	AtStart bool
	// Preceding is the character immediately before the cursor.
	// Valid only when HasContext is true and AtStart is false.
	// (UTF-16 code unit cast to rune — non-BMP characters become
	// surrogates, but Russian/English insertion points are always
	// BMP so this is fine in practice.)
	Preceding rune
	// Status is a diagnostic label describing which AX step
	// succeeded last — useful in the log when a transform did
	// nothing and we want to know why (webview blocked us? no
	// focus? at start?).
	Status string
}
