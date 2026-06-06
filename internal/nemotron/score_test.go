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

func TestHybridScoreRawTiebreak(t *testing.T) {
	// Same text, better (less-negative) raw should win.
	a := HybridScore("одинаковый текст.", -500)
	b := HybridScore("одинаковый текст.", -2000)
	if a <= b {
		t.Fatalf("less-negative raw (%.2f) should beat more-negative (%.2f)", a, b)
	}
}
