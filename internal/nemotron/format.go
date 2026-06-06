//go:build linux

package nemotron

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormatNemotron post-processes one raw sidecar transcription for insertion:
//  1. transliterate Cyrillic term spellings to canonical Latin (Kafka, …);
//  2. for phrases over 5 words, ensure a leading capital and a terminal
//     ./!/? — the model occasionally drops them on otherwise clean speech
//     (short fragments are left as-is, like Whisper's pipeline);
//  3. append a trailing space so consecutive dictations don't glue together.
//
// Deliberately minimal: Nemotron lacks Whisper's stutter/hallucination
// failure modes, so we don't port those heuristics.
func FormatNemotron(text string) string {
	t := Transliterate(strings.TrimSpace(text), nil)
	if t == "" {
		return ""
	}
	if len(strings.Fields(t)) > 5 {
		t = ensureLeadingCapital(t)
		t = ensureTerminalPunct(t)
	}
	return t + " "
}

func ensureLeadingCapital(t string) string {
	r, size := utf8.DecodeRuneInString(t)
	if r == utf8.RuneError || !unicode.IsLetter(r) || unicode.IsUpper(r) {
		return t
	}
	return string(unicode.ToUpper(r)) + t[size:]
}

func ensureTerminalPunct(t string) string {
	trimmed := strings.TrimRight(t, " ")
	if trimmed == "" {
		return t
	}
	switch r, _ := utf8.DecodeLastRuneInString(trimmed); r {
	case '.', '!', '?', '…':
		return trimmed
	}
	return trimmed + "."
}
