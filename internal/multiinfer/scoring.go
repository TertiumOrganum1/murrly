package multiinfer

import (
	"strings"

	"github.com/tertiumorganum1/murrly/internal/crossjudge"
)

// ScoreMode selects WHICH metric ranks the variants and picks the one to
// insert. There is no hidden third metric: the only two signals are the ones
// the picker shows — the model's own confidence (1st number) and our
// 7-criteria cross score (2nd number). The menu choice is the principle from
// selection all the way to the ★/✓ marks.
type ScoreMode int

const (
	// ScoreCombined — model confidence + our cross score.
	ScoreCombined ScoreMode = iota
	// ScoreConfidence — the model's mean per-token probability only.
	ScoreConfidence
	// ScoreHeuristic — our 7-criteria cross score only ("только наша метрика").
	ScoreHeuristic
)

// ParseScoreMode maps a config string to a ScoreMode, defaulting to
// combined for empty/unknown values.
func ParseScoreMode(s string) ScoreMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "confidence", "whisper", "probability":
		return ScoreConfidence
	case "heuristic", "empirical", "text", "ours", "cross":
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

// crossNormDivisor maps our raw cross score into ~[0,1] for the combined
// blend (clean Russian lands ~0.3–0.5; latin terms / punctuation push higher).
const crossNormDivisor = 3.0

// score ranks a candidate under the chosen mode, using exactly the two
// metrics surfaced in the picker — confidence (1st number) and our cross
// score (2nd number). ScoreHeuristic returns the raw cross score (only the
// ordering matters); the others stay in [0,1].
func score(text string, confidence float64, mode ScoreMode) float64 {
	switch mode {
	case ScoreConfidence:
		return clamp01(confidence)
	case ScoreHeuristic:
		return crossjudge.Score(text, "")
	default:
		return 0.5*clamp01(confidence) + 0.5*clamp01(crossjudge.Score(text, "")/crossNormDivisor)
	}
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
