package uicontext

import "testing"

func TestApplyPassThroughWhenContextUnavailable(t *testing.T) {
	got := Apply("Hello world. ", Context{HasContext: false})
	want := "Hello world. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestApplyPassThroughOnEmptyText(t *testing.T) {
	got := Apply("", Context{HasContext: true, AtStart: true})
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestApplyCapitalisesAtStartOfDocument(t *testing.T) {
	got := Apply("привет. ", Context{HasContext: true, AtStart: true})
	want := "Привет. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestApplyCapitalisesAfterTerminator(t *testing.T) {
	for _, prev := range []rune{'.', '!', '?'} {
		ctx := Context{HasContext: true, Preceding: prev}
		got := Apply("привет. ", ctx)
		want := " Привет. "
		if got != want {
			t.Errorf("preceding=%q: got %q, want %q", prev, got, want)
		}
	}
}

func TestApplyLowercasesMidSentenceAfterLetter(t *testing.T) {
	ctx := Context{HasContext: true, Preceding: 'а'}
	got := Apply("Который я решил. ", ctx)
	want := " который я решил"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestApplyLowercasesMidSentenceAfterComma(t *testing.T) {
	ctx := Context{HasContext: true, Preceding: ','}
	got := Apply("Который я решил. ", ctx)
	want := " который я решил"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestApplyLeavesAloneWhenPrecededByWhitespace(t *testing.T) {
	ctx := Context{HasContext: true, Preceding: ' '}
	got := Apply("Привет. ", ctx)
	want := "Привет. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestApplyAddsLeadingSpaceAfterDigit(t *testing.T) {
	ctx := Context{HasContext: true, Preceding: '7'}
	got := Apply("Дней назад. ", ctx)
	want := " дней назад"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Already-lowercase first letter shouldn't be touched when we'd
// only be reapplying the same case.
func TestApplyDoesNotOverCorrectAlreadyCorrectCase(t *testing.T) {
	// mid-sentence, first letter already lowercase
	ctx := Context{HasContext: true, Preceding: 'а'}
	got := Apply("который я решил. ", ctx)
	want := " который я решил"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// start-of-document, first letter already uppercase
	ctx = Context{HasContext: true, AtStart: true}
	got = Apply("Привет. ", ctx)
	want = "Привет. "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Non-letter first character (digit, punctuation): just apply spacing
// rules, no casing change.
func TestApplyHandlesNonLetterStart(t *testing.T) {
	ctx := Context{HasContext: true, Preceding: 'а'}
	got := Apply("100 рублей. ", ctx)
	want := " 100 рублей"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
