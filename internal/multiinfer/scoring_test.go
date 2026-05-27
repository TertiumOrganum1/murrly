package multiinfer

import "testing"

func TestHeuristicRanking(t *testing.T) {
	clean := "Мы обсуждаем React и Docker в микросервисах."
	fastMode := "мы обсуждаем реакт и докер в микросервисах а ещё кубернетес и всякое такое без знаков препинания совсем длинная фраза"
	cyrillicTerms := "Мы обсуждаем Реакт и Докер в микросервисах."
	short := "Да."

	hClean := heuristic(clean)
	hFast := heuristic(fastMode)
	hCyr := heuristic(cyrillicTerms)
	hShort := heuristic(short)

	if hClean <= hFast {
		t.Errorf("clean (%.3f) should outrank fast-mode (%.3f)", hClean, hFast)
	}
	if hClean <= hCyr {
		t.Errorf("Latin-terms (%.3f) should outrank Cyrillic-terms (%.3f)", hClean, hCyr)
	}
	if hShort >= hClean {
		t.Errorf("short (%.3f) should rank below clean (%.3f)", hShort, hClean)
	}
}

func TestHeuristicShortFloor(t *testing.T) {
	if got := heuristic("Привет."); got != 0.1 {
		t.Errorf("sub-3-word text should floor to 0.1, got %.3f", got)
	}
}

func TestScoreCombines5050(t *testing.T) {
	// Two texts with identical heuristic but different confidence: the
	// higher-confidence one must win under the combined mode.
	text := "Мы обсуждаем React и Docker в микросервисах."
	low := score(text, 0.2, ScoreCombined)
	high := score(text, 0.9, ScoreCombined)
	if high <= low {
		t.Errorf("higher confidence should score higher: low=%.3f high=%.3f", low, high)
	}
}

func TestScoreModes(t *testing.T) {
	text := "Мы обсуждаем React и Docker в микросервисах."
	const conf = 0.42

	// Confidence mode ignores text shape entirely.
	if got := score(text, conf, ScoreConfidence); got != clamp01(conf) {
		t.Errorf("confidence mode = %.3f, want %.3f", got, conf)
	}
	// Heuristic mode ignores the confidence number entirely.
	if got := score(text, conf, ScoreHeuristic); got != heuristic(text) {
		t.Errorf("heuristic mode = %.3f, want %.3f", got, heuristic(text))
	}
	// Combined sits between the two single signals here (heuristic > conf).
	combined := score(text, conf, ScoreCombined)
	if combined <= conf || combined >= heuristic(text) {
		t.Errorf("combined %.3f should sit between conf %.3f and heuristic %.3f", combined, conf, heuristic(text))
	}
}

func TestParseScoreMode(t *testing.T) {
	cases := map[string]ScoreMode{
		"":           ScoreCombined,
		"combined":   ScoreCombined,
		"garbage":    ScoreCombined,
		"confidence": ScoreConfidence,
		"Whisper":    ScoreConfidence,
		"heuristic":  ScoreHeuristic,
		"  empirical ": ScoreHeuristic,
	}
	for in, want := range cases {
		if got := ParseScoreMode(in); got != want {
			t.Errorf("ParseScoreMode(%q) = %v, want %v", in, got, want)
		}
		// Canonical strings must round-trip.
		if got := ParseScoreMode(want.String()); got != want {
			t.Errorf("round-trip %v -> %q -> %v failed", want, want.String(), got)
		}
	}
}

func TestLatinRatio(t *testing.T) {
	cases := []struct {
		text string
		min  float64
		max  float64
	}{
		{"полностью русский текст", 0, 0.01},
		{"React Docker Kubernetes", 0.99, 1},
	}
	for _, c := range cases {
		got := latinRatio(c.text)
		if got < c.min || got > c.max {
			t.Errorf("latinRatio(%q)=%.3f, want in [%.2f,%.2f]", c.text, got, c.min, c.max)
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
