package uicontext

import (
	"strings"
	"unicode"
)

// sentenceEnders are the characters after which a fresh sentence
// starts: the new text keeps its capital and its own terminator.
const sentenceEnders = ".!?…"

// midConnectors join clauses: inserting after one means we're inside a
// sentence — lower-case the first letter and drop our terminator.
const midConnectors = ",;:"

// openBrackets: inserting right after one is mid-sentence, but with NO
// leading space — the text sits flush against the bracket.
const openBrackets = "([{<"

// dashes: an em/en dash before the insertion point continues the clause,
// like a connector (lower-case, leading space unless one is already there).
const dashes = "—–"

// tailMode says what happens to the end of the inserted text.
type tailMode int

const (
	// tailNone — leave the tail exactly as the pipeline produced it.
	tailNone tailMode = iota
	// tailKeep — sentence boundary: the terminator stays, but the
	// trailing blank is dropped when the field already provides one
	// (or nothing follows at all).
	tailKeep
	// tailStrip — mid-sentence insertion: strip ALL trailing
	// punctuation ("." / "..." / "…" / "!" / "?" / combinations) plus
	// blanks, then re-add a single space only if a word follows
	// immediately. Exception: when nothing follows (caret at the end of
	// the field) the phrase ends the sentence, so its terminator is kept
	// (only the trailing blank is dropped, like tailKeep).
	tailStrip
)

// Apply rewrites the recognised text to fit the cursor's surroundings.
// Pure function — depends only on text and ctx, no IO. When
// ctx.HasContext is false the text is returned untouched (we couldn't
// read the focus, so don't guess).
//
// Left side (what's before the insertion point) decides the head:
//
//	nothing / line start       → capitalise, no leading space
//	`.` `!` `?` `…`            → capitalise, leading space unless one
//	                             is already there
//	letter / digit / `,;:`     → lower-case, leading space unless
//	                             already there, mid-sentence tail
//	raw space (macOS capture)  → ambiguous — leave alone
//	anything else              → leave alone
//
// Right side (what's after the insertion point) decides the tail —
// see tailMode. The guiding rule, per the dictation UX: when slipping
// a phrase into the middle of a sentence exactly one space must
// separate it from the next word, counting any space that was already
// there; sentence-final punctuation of the inserted phrase disappears
// because the sentence it lands in already has its own.
//
// "Capitalise" only flips Lower → Upper; if the first letter is
// already upper or non-letter, nothing happens. Same for the inverse
// (proper names at the start of a mid-sentence insert do get
// lower-cased — we can't tell a name from a sentence-start capital
// without a dictionary, an accepted limitation).
func Apply(text string, ctx Context) string {
	if !ctx.HasContext || text == "" {
		return text
	}
	runes := []rune(text)

	capitalize := false
	lowercase := false
	leadSpace := false
	tail := tailNone

	switch {
	case ctx.ForceMid:
		// Unconditional mid-sentence insert (Shift+F12): no field was read,
		// so always lower-case, always lead with one space, strip the
		// phrase's terminator. RightKnown is false here, so tailStrip drops
		// the terminator without trying to manage a trailing space.
		lowercase = true
		leadSpace = !ctx.SpaceBefore
		tail = tailStrip
	case ctx.AtStart, ctx.Preceding == '\n':
		capitalize = true
		tail = tailKeep
	case strings.ContainsRune(sentenceEnders, ctx.Preceding):
		capitalize = true
		leadSpace = !ctx.SpaceBefore
		tail = tailKeep
	case unicode.IsLetter(ctx.Preceding) || unicode.IsDigit(ctx.Preceding):
		lowercase = true
		leadSpace = !ctx.SpaceBefore
		tail = tailStrip
	case strings.ContainsRune(midConnectors, ctx.Preceding):
		lowercase = true
		leadSpace = !ctx.SpaceBefore
		tail = tailStrip
	case strings.ContainsRune(openBrackets, ctx.Preceding):
		// Right after an opening bracket — mid-sentence, but never a leading
		// space (the text sits flush against the bracket). Lower-case the
		// first letter and drop the phrase's own terminal punctuation.
		lowercase = true
		tail = tailStrip
	case strings.ContainsRune(dashes, ctx.Preceding):
		// After a dash (—, –) the phrase continues the sentence/clause.
		lowercase = true
		leadSpace = !ctx.SpaceBefore
		tail = tailStrip
	case unicode.IsSpace(ctx.Preceding):
		// Only whitespace to the left — a field holding just blanks, or a
		// blank that isn't a plain newline. Treat it as a fresh start, NOT a
		// mid-sentence insert: capitalise, keep the phrase's own terminal
		// punctuation, no leading space. Never decapitalise or strip here.
		// (AtStart and '\n' already take the start path above; this covers
		// the remaining whitespace cases.)
		capitalize = true
		tail = tailKeep
	default:
		// Brackets, quotes, emoji, dashes… — no safe guess.
		return text
	}

	if capitalize && unicode.IsLower(runes[0]) {
		runes[0] = unicode.ToUpper(runes[0])
	}
	if lowercase && unicode.IsUpper(runes[0]) {
		runes[0] = unicode.ToLower(runes[0])
	}
	out := string(runes)

	switch tail {
	case tailStrip:
		if ctx.RightKnown && ctx.AtEnd {
			// Nothing follows the caret — the inserted phrase IS the end of
			// the sentence, so it must keep its own terminator. (Stripping it
			// here is what left "…отцы и деды" with no period when appending
			// to the end of a field.) Drop only the trailing blank, exactly
			// like tailKeep; the head still gets lower-cased / lead-spaced.
			out = strings.TrimRight(out, " \t")
			break
		}
		stripped := strings.TrimRightFunc(out, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r)
		})
		// A transcription that is ALL punctuation would vanish —
		// keep it untrimmed instead of inserting nothing.
		if stripped != "" {
			out = stripped
			if ctx.RightKnown && !ctx.AtEnd &&
				(unicode.IsLetter(ctx.Following) || unicode.IsDigit(ctx.Following)) {
				out += " "
			}
		}
	case tailKeep:
		if ctx.RightKnown && (ctx.AtEnd || unicode.IsSpace(ctx.Following)) {
			out = strings.TrimRight(out, " \t")
		}
	}

	if leadSpace && !strings.HasPrefix(out, " ") {
		out = " " + out
	}
	return out
}
