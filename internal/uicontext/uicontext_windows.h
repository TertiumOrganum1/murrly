#ifndef MUR_UICONTEXT_WINDOWS_H
#define MUR_UICONTEXT_WINDOWS_H

#ifdef __cplusplus
extern "C" {
#endif

// MurUICtx mirrors the Go uicontext.Context fields the transform needs.
// Character fields carry a UTF-16/BMP code point (Cyrillic and the ASCII
// punctuation/space the rules care about are all single-unit), 0 = none.
typedef struct {
	int hasContext;  // 1 when a text control with a caret/selection was read
	int atStart;     // nothing meaningful precedes the insertion point
	int rightKnown;  // the right side was read (always 1 when hasContext)
	int atEnd;       // nothing follows the insertion point
	int spaceBefore; // one or more blanks sit immediately to the left
	int preceding;   // first non-blank code point to the left ('\n' = line start)
	int following;   // code point immediately to the right (not blank-skipped)
	int stage;       // diagnostic: how far capture got (see uicontext_windows.cpp)
} MurUICtx;

// mur_uictx_capture fills *out from the focused UI Automation element.
// Returns 1 on a successful read (out->hasContext also set), 0 otherwise.
int mur_uictx_capture(MurUICtx* out);

#ifdef __cplusplus
}
#endif

#endif
