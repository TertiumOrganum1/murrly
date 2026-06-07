//go:build linux

package nemotron

import "testing"

func TestHybridScoreEmptyIsWorst(t *testing.T) {
	if HybridScore("", 0) > HybridScore("привет.", -100) {
		t.Fatal("empty text should score worse than any real text")
	}
}

func TestHybridScorePrefersPunctuatedCapitalised(t *testing.T) {
	clean := HybridScore("Привет, как дела?", -1000)
	messy := HybridScore("привет как дела", -1000)
	if clean <= messy {
		t.Fatalf("punctuated+capitalised (%.2f) should beat bare (%.2f)", clean, messy)
	}
}

func TestHybridScorePenalisesStutter(t *testing.T) {
	normal := HybridScore("дежурный инженер идёт смотреть.", -1000)
	stutter := HybridScore("идем идем идем южный инженер.", -1000)
	if stutter >= normal {
		t.Fatalf("stutter (%.2f) should score below clean (%.2f)", stutter, normal)
	}
}

func TestHybridScoreConfidenceTiebreak(t *testing.T) {
	// Same text, higher per-token confidence should win.
	a := HybridScore("одинаковый текст.", 0.9)
	b := HybridScore("одинаковый текст.", 0.5)
	if a <= b {
		t.Fatalf("higher confidence (%.2f) should beat lower (%.2f)", a, b)
	}
}
