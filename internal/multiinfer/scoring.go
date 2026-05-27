package multiinfer

import (
	"strings"
	"unicode"
)

// ScoreMode selects how a candidate's rank is computed. The combined
// default mixes Whisper's confidence with our text-shape heuristic; the
// two single-signal modes exist because either one alone can pick a
// better variant depending on the audio, and the user wanted to compare
// them live from the tray menu rather than guess at fixed weights.
type ScoreMode int

const (
	// ScoreCombined — 0.5*confidence + 0.5*heuristic (the original blend).
	ScoreCombined ScoreMode = iota
	// ScoreConfidence — Whisper's mean per-token probability only.
	ScoreConfidence
	// ScoreHeuristic — the text-shape heuristic only (length, punctuation,
	// capitalization, Latin-term preservation).
	ScoreHeuristic
)

// ParseScoreMode maps a config string to a ScoreMode, defaulting to
// combined for empty/unknown values.
func ParseScoreMode(s string) ScoreMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "confidence", "whisper", "probability":
		return ScoreConfidence
	case "heuristic", "empirical", "text":
		return ScoreHeuristic
	default:
		return ScoreCombined
	}
}

// String is the canonical config token for a mode (round-trips through
// ParseScoreMode).
func (m ScoreMode) String() string {
	switch m {
	case ScoreConfidence:
		return "confidence"
	case ScoreHeuristic:
		return "heuristic"
	default:
		return "combined"
	}
}

// score ranks a candidate in [0,1] under the chosen mode. Confidence
// alone is a weak signal (Whisper is often confidently wrong on fast-mode
// mush); the heuristic catches the failure shapes the user actually sees
// — junk-short output, fast-mode (no punctuation / no capitals), and
// technical terms rendered in Cyrillic instead of Latin. The combined
// mode averages the two; the single-signal modes let the user pick when
// the blend misbehaves.
func score(text string, confidence float64, mode ScoreMode) float64 {
	switch mode {
	case ScoreConfidence:
		return clamp01(confidence)
	case ScoreHeuristic:
		return heuristic(text)
	default:
		return clamp01(0.5*clamp01(confidence) + 0.5*heuristic(text))
	}
}

// heuristic scores text quality in [0,1] from its shape alone.
func heuristic(text string) float64 {
	text = strings.TrimSpace(text)
	words := strings.Fields(text)
	if len(words) < 3 {
		// Too short to be a real dictation result — almost certainly a
		// dropped/garbled decode. Floor it so any fuller candidate wins.
		return 0.1
	}

	s := 0.5 // base

	punct := hasSentencePunct(text)
	upper := hasUppercase(text)

	// Fast-mode: a long run with neither sentence punctuation nor any
	// capital letter is Whisper's degraded output. Strong penalty.
	if len(words) >= 15 && !punct && !upper {
		s -= 0.4
	}

	// Formatting bonuses — present punctuation and capitalization both
	// signal a clean decode.
	if punct {
		s += 0.15
	}
	if upper {
		s += 0.15
	}

	// Latin presence: dictation here is Russian with embedded English
	// technical terms (React, Docker, …). A candidate that preserved
	// those in Latin scores higher than one that Cyrillicized them.
	// Scaled by the Latin share of all letters, capped at +0.2.
	s += 0.2 * latinRatio(text)

	return clamp01(s)
}

func hasSentencePunct(text string) bool {
	return strings.ContainsAny(text, ".!?")
}

func hasUppercase(text string) bool {
	for _, r := range text {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// latinRatio returns the fraction of letters that are Latin-script,
// over all letters (Latin + Cyrillic + others). 0 for pure Russian
// (no penalty — just no bonus), higher when English terms survived.
func latinRatio(text string) float64 {
	var latin, letters int
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Latin, r) {
			latin++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(latin) / float64(letters)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
