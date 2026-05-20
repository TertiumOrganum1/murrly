package transcriber

import (
	"strings"
	"testing"
)

func TestFormatSegmentsJoinsTrimmedWhisperSegments(t *testing.T) {
	got := formatSegments([]string{
		"Первое предложение.",
		"Второе предложение.",
	})
	want := "Первое предложение. Второе предложение. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsAddsMissingSentenceSpacing(t *testing.T) {
	got := formatSegments([]string{"первое предложение.второе?Третье!Четвертое"})
	want := "первое предложение. второе? Третье! Четвертое"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsKeepsDecimalsAndAbbreviations(t *testing.T) {
	got := formatSegments([]string{"Версия 3.14 работает т.е. корректно"})
	want := "Версия 3.14 работает т.е. корректно"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsKeepsTechnicalDots(t *testing.T) {
	got := formatSegments([]string{"github.com, README.md и Node.js не меняются"})
	want := "github.com, README.md и Node.js не меняются"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsAddsSpaceAfterSingleFinishedSentence(t *testing.T) {
	got := formatSegments([]string{"Готово."})
	want := "Готово. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsLeavesUnfinishedSentenceWithoutTrailingSpace(t *testing.T) {
	got := formatSegments([]string{"Готово"})
	want := "Готово"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsDropsHallucinationOnlyPhrases(t *testing.T) {
	tests := []string{
		"Продолжение следует...",
		"Субтитры сделал DimaTorzok.",
		"Редактор субтитров А. Семкин.",
		"Thanks for watching!",
		"Subtitles by the Amara.org community.",
		"В этом видео я покажу, как сделать.",
		"Это все, что я могу сказать.",
		"Ну, и, конечно, это не все.",
	}
	for _, input := range tests {
		if got := formatSegments([]string{input}); got != "" {
			t.Fatalf("formatSegments(%q) = %q, want empty", input, got)
		}
	}
}

func TestFormatSegmentsDropsTrailingHallucinationPhrases(t *testing.T) {
	got := formatSegments([]string{"Сборка прошла. Продолжение следует..."})
	want := "Сборка прошла. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = formatSegments([]string{"Проверь логи. Субтитры сделал DimaTorzok."})
	want = "Проверь логи. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = formatSegments([]string{"Сборка прошла. В этом видео я покажу, как сделать."})
	want = "Сборка прошла. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsCollapsesRepeatedSentences(t *testing.T) {
	got := formatSegments([]string{
		"Проверь сборку. Дальше надо смотреть логи. Дальше надо смотреть логи. Дальше надо смотреть логи. Дальше надо смотреть логи.",
	})
	want := "Проверь сборку. Дальше надо смотреть логи. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = formatSegments([]string{
		"Начало. Дальше надо смотреть логи. Дальше надо смотреть логи. Дальше надо смотреть логи. Потом проверить сервис.",
	})
	want = "Начало. Дальше надо смотреть логи. Потом проверить сервис. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsCollapsesRepeatedWords(t *testing.T) {
	got := formatSegments([]string{"Проверь логи логи логи логи и сервис."})
	want := "Проверь логи и сервис. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = formatSegments([]string{"Да да да проверь."})
	want = "Да проверь. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsCollapsesTinyRepeatedSentences(t *testing.T) {
	got := formatSegments([]string{"Да. Да. Да."})
	want := "Да. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsKeepsShortDatasetPhrases(t *testing.T) {
	tests := []string{"you", "bye", "good morning", "I am not sure if this is the right way."}
	for _, input := range tests {
		want := input
		if strings.HasSuffix(input, ".") {
			want += " "
		}
		if got := formatSegments([]string{input}); got != want {
			t.Fatalf("formatSegments(%q) = %q, want %q", input, got, want)
		}
	}
}
