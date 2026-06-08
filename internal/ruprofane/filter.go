// Package ruprofane roughly censors Russian obscene language (мат) in text.
//
// It is a VIEW transform, applied only at display (picker plaques) and at
// insertion — gated by a runtime toggle (tray checkbox), off by default. The
// recognized text is always stored and scored uncensored, so a false positive
// never destroys the phrase: untick the box and the original reappears.
//
// Dictionary: the maintained dataset ddfdi/russian_ban_words (Dmitry Frolov,
// MIT — see data/LICENSE.txt), files ru_curse_words.txt (obscene forms) and
// ru_exception_words.txt (innocent words that merely contain an obscene
// substring: страхуй, мандат, …). We match WHOLE WORDS against the curse set
// (case-insensitive, ё folded to е) minus the exceptions — this catches real
// mat without censoring innocent words that share a substring (рубля, сабля,
// корабля). Matched words are masked keeping the first letter, bullets
// so even a rare false positive stays legible.
package ruprofane

import (
	_ "embed"
	"regexp"
	"strings"
	"sync/atomic"
)

//go:embed data/ru_curse_words.txt
var curseData string

//go:embed data/ru_exception_words.txt
var exceptionData string

var (
	curseSet     = parseSet(curseData)
	exceptionSet = parseSet(exceptionData)
	wordRe       = regexp.MustCompile(`[\p{L}]+`)
	enabled      atomic.Bool
)

// SetEnabled turns the filter on/off at runtime (tray checkbox).
func SetEnabled(on bool) { enabled.Store(on) }

// Enabled reports the current state (for menu rendering / persistence).
func Enabled() bool { return enabled.Load() }

// Filter masks obscene whole words when enabled; returns text unchanged when
// disabled. Per-word, preserving punctuation and spacing.
func Filter(text string) string {
	if !enabled.Load() {
		return text
	}
	return wordRe.ReplaceAllStringFunc(text, func(w string) string {
		key := fold(w)
		if _, ok := exceptionSet[key]; ok {
			return w
		}
		if _, ok := curseSet[key]; ok {
			return maskWord(w)
		}
		return w
	})
}

// maskChar is the bullet (U+2022), used instead of '*' so the mask never
// collides with asterisk markup or other meaningful punctuation.
const maskChar = "•"

// maskWord keeps the first rune and replaces the rest with bullets.
func maskWord(w string) string {
	r := []rune(w)
	if len(r) <= 1 {
		return w
	}
	return string(r[0]) + strings.Repeat(maskChar, len(r)-1)
}

// fold lowercases and maps ё→е so spelling variants collapse to one key.
func fold(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ё", "е") }

func parseSet(data string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, line := range strings.Split(data, "\n") {
		w := strings.TrimSpace(line)
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		m[fold(w)] = struct{}{}
	}
	return m
}
