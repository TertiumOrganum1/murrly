package multiinfer

import (
	"testing"

	"github.com/tertiumorganum1/murrly/internal/crossjudge"
)

func TestScoreConfidenceMode(t *testing.T) {
	text := "Мы обсуждаем Docker и Kubernetes."
	if got := score(text, 0.42, ScoreConfidence); got != clamp01(0.42) {
		t.Errorf("confidence mode = %.3f, want %.3f", got, 0.42)
	}
}

func TestScoreHeuristicModeIsOurCrossScore(t *testing.T) {
	// "Только наша метрика" must rank purely by our 7-criteria cross score —
	// no hidden internal heuristic, and the confidence number is ignored.
	text := "Мы обсуждаем Docker и Kubernetes."
	if got := score(text, 0.42, ScoreHeuristic); got != crossjudge.Score(text, "") {
		t.Errorf("heuristic mode = %.3f, want cross score %.3f", got, crossjudge.Score(text, ""))
	}
}

func TestScoreCombinedUsesConfidence(t *testing.T) {
	// Same text, higher confidence ⇒ higher combined score (confidence is
	// half the blend).
	text := "Мы обсуждаем Docker и Kubernetes."
	low := score(text, 0.2, ScoreCombined)
	high := score(text, 0.9, ScoreCombined)
	if high <= low {
		t.Errorf("combined should rise with confidence: low=%.3f high=%.3f", low, high)
	}
}

func TestParseScoreMode(t *testing.T) {
	cases := map[string]ScoreMode{
		"":             ScoreCombined,
		"combined":     ScoreCombined,
		"garbage":      ScoreCombined,
		"confidence":   ScoreConfidence,
		"Whisper":      ScoreConfidence,
		"heuristic":    ScoreHeuristic,
		"  empirical ": ScoreHeuristic,
		"ours":         ScoreHeuristic,
	}
	for in, want := range cases {
		if got := ParseScoreMode(in); got != want {
			t.Errorf("ParseScoreMode(%q) = %v, want %v", in, got, want)
		}
	}
	for _, m := range []ScoreMode{ScoreCombined, ScoreConfidence, ScoreHeuristic} {
		if ParseScoreMode(m.String()) != m {
			t.Errorf("round-trip %v -> %q failed", m, m.String())
		}
	}
}

func TestPadPCM(t *testing.T) {
	in := []float32{1, 2, 3}
	out := padPCM(in, 1.0, 0.5) // 1s lead = 16000, 0.5s trail = 8000
	wantLen := 16000 + 3 + 8000
	if len(out) != wantLen {
		t.Fatalf("padded length = %d, want %d", len(out), wantLen)
	}
	if out[16000] != 1 || out[16001] != 2 || out[16002] != 3 {
		t.Errorf("original samples not preserved at offset 16000")
	}
	if out[0] != 0 || out[len(out)-1] != 0 {
		t.Errorf("pad regions should be zero")
	}
}
