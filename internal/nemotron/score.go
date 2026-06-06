//go:build linux

package nemotron

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var sentenceEndRe = regexp.MustCompile(`[.!?]["»)]?\s*$`)

// HybridScore ranks a Nemotron candidate: higher = better. It blends the
// model's raw RNNT hypothesis score (a length-scaled negative log-prob)
// with cheap text-quality heuristics — used to pick the best of several
// diversified variants of the SAME utterance, where the texts are similar
// and the cleanest (punctuated, capitalised, no stutter) should win.
//
// We deliberately do NOT reuse Whisper's metric here; Nemotron's strengths
// and failure modes differ (it doesn't slide into Whisper's "simple mode",
// but can stutter or drop a final period on short fragments).
func HybridScore(text string, raw float64) float64 {
	t := strings.TrimSpace(text)
	if t == "" {
		return -1e18
	}
	// Raw score is large-negative and grows with length; a small weight
	// makes it a tiebreak among same-utterance variants without letting it
	// swamp the heuristics.
	score := raw * 0.0002
	if sentenceEndRe.MatchString(t) {
		score += 3 // ends on a real terminator
	}
	if c := strings.Count(t, ","); c > 0 {
		if c > 4 {
			c = 4
		}
		score += float64(c) // internal punctuation = structure
	}
	if r, _ := utf8.DecodeRuneInString(t); unicode.IsUpper(r) {
		score += 1 // capitalised sentence start
	}
	score -= 4 * float64(repeatedWordRuns(t)) // stutter / loop penalty
	return score
}

// repeatedWordRuns counts adjacent duplicate words (case-insensitive),
// the signature of an RNNT stutter loop ("идем идем идем").
func repeatedWordRuns(text string) int {
	words := strings.Fields(strings.ToLower(text))
	n := 0
	for i := 1; i < len(words); i++ {
		if words[i] == words[i-1] {
			n++
		}
	}
	return n
}
