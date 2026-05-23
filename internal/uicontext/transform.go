package uicontext

import (
	"strings"
	"unicode"
)

// Apply rewrites the recognised text to fit the cursor's surroundings.
// Pure function — depends only on text and ctx, no IO. When
// ctx.HasContext is false the text is returned untouched (we couldn't
// read the focus, so don't guess).
//
// Rules, based on the character immediately to the left of the cursor:
//
//	at start of doc          → capitalise first letter
//	`.` / `!` / `?`          → capitalise first letter, prepend " "
//	letter / digit           → lower-case first letter, prepend " ",
//	                           strip a trailing ". " (we're inserting
//	                           mid-sentence, a terminator would chop it)
//	`,` / `;` / `:`          → lower-case first letter, prepend " ",
//	                           strip trailing ". "
//	whitespace               → leave alone — could be either between
//	                           sentences or just after a comma; without
//	                           looking further back we can't decide,
//	                           so we don't risk a bad transform
//	anything else            → leave alone
//
// "Capitalise" only flips Lower → Upper; if the first letter is
// already upper or non-letter, nothing happens. Same for the inverse.
// Whisper's filter pipeline already gets first-letter casing right in
// most cases — Apply only corrects the edge cases where the cursor
// context disagrees with the standalone-sentence assumption.
func Apply(text string, ctx Context) string {
	if !ctx.HasContext || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}

	addLeadingSpace := false
	capitalise := false
	lowercase := false
	stripTerminator := false

	switch {
	case ctx.AtStart:
		capitalise = true
	case ctx.Preceding == '.' || ctx.Preceding == '!' || ctx.Preceding == '?':
		capitalise = true
		addLeadingSpace = true
	case unicode.IsSpace(ctx.Preceding):
		// Ambiguous — leave casing alone, no leading space needed.
	case unicode.IsLetter(ctx.Preceding) || unicode.IsDigit(ctx.Preceding):
		lowercase = true
		addLeadingSpace = true
		stripTerminator = true
	case ctx.Preceding == ',' || ctx.Preceding == ';' || ctx.Preceding == ':':
		lowercase = true
		addLeadingSpace = true
		stripTerminator = true
	}

	if capitalise && unicode.IsLower(runes[0]) {
		runes[0] = unicode.ToUpper(runes[0])
	}
	if lowercase && unicode.IsUpper(runes[0]) {
		runes[0] = unicode.ToLower(runes[0])
	}
	result := string(runes)

	if stripTerminator {
		// finalizeTerminalPunctuation in the transcriber pipeline
		// always appends ". " at the end; mid-sentence insertion
		// needs to chop that back off.
		result = strings.TrimRight(result, " ")
		result = strings.TrimRight(result, ".!?")
	}

	if addLeadingSpace {
		result = " " + result
	}
	return result
}
