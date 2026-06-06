//go:build linux

package nemotron

import "testing"

func TestTransliterateWholeWord(t *testing.T) {
	got := Transliterate("каждый сервис экспортит метрики в прометеус", nil)
	want := "каждый сервис экспортит метрики в Prometheus"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTransliterateCaseInsensitive(t *testing.T) {
	if got := Transliterate("Кафка и Редис", nil); got != "Kafka и Redis" {
		t.Fatalf("got %q", got)
	}
}

func TestTransliterateLeavesNonMatchesAndPunctuation(t *testing.T) {
	// "мешает" must not be touched by any "меш*" key; punctuation preserved.
	if got := Transliterate("это мешает, ок.", nil); got != "это мешает, ок." {
		t.Fatalf("got %q", got)
	}
}

func TestFormatLongPhraseGetsCapitalAndPeriod(t *testing.T) {
	got := FormatNemotron("каждый сервис экспортит метрики в прометеус")
	want := "Каждый сервис экспортит метрики в Prometheus. "
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatShortPhraseLeftAloneButSpaced(t *testing.T) {
	// 3 words (<=5): no forced capital/period, but trailing space added.
	if got := FormatNemotron("привет как дела"); got != "привет как дела " {
		t.Fatalf("got %q", got)
	}
}

func TestFormatEmpty(t *testing.T) {
	if got := FormatNemotron("   "); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatPreservesExistingTerminal(t *testing.T) {
	got := FormatNemotron("Это уже законченное предложение с точкой.")
	if got != "Это уже законченное предложение с точкой. " {
		t.Fatalf("got %q", got)
	}
}
