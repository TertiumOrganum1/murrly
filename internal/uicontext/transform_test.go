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

// Table-driven core: one row per rule from the context-insert spec.
func TestApplyRules(t *testing.T) {
	cases := []struct {
		name string
		text string
		ctx  Context
		want string
	}{
		// --- left side: start of field / replace-all / line start ---
		{
			name: "start of empty field capitalises, no leading space",
			text: "привет. ",
			ctx:  Context{HasContext: true, AtStart: true},
			want: "Привет. ",
		},
		{
			name: "replace-all (select-all) behaves as empty field, keeps trailing space",
			text: "привет. ",
			ctx:  Context{HasContext: true, AtStart: true, RightKnown: true, AtEnd: true},
			want: "Привет. ",
		},
		{
			name: "line start (after newline) capitalises, keeps trailing space",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '\n', RightKnown: true, AtEnd: true},
			want: "Привет. ",
		},

		// --- left side: after sentence terminator ---
		{
			name: "after period adds leading space and capitalises",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '.'},
			want: " Привет. ",
		},
		{
			name: "after ellipsis char capitalises",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '…'},
			want: " Привет. ",
		},
		{
			name: "after period with existing space adds no second space",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '.', SpaceBefore: true},
			want: "Привет. ",
		},
		{
			name: "sentence at end of field keeps terminator and trailing space",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '.', RightKnown: true, AtEnd: true},
			want: " Привет. ",
		},
		{
			name: "sentence before existing space keeps period, drops own blank",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '.', RightKnown: true, Following: ' '},
			want: " Привет.",
		},
		{
			name: "sentence inserted right before a word keeps period+space",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '.', RightKnown: true, Following: 'С'},
			want: " Привет. ",
		},

		// --- left side: mid-sentence (letter / digit / connector) ---
		{
			name: "mid-sentence after letter: lowercase, lead space, strip terminator",
			text: "Который я решил. ",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " который я решил",
		},
		{
			name: "mid-sentence after digit",
			text: "Дней назад. ",
			ctx:  Context{HasContext: true, Preceding: '7'},
			want: " дней назад",
		},
		{
			name: "mid-sentence after comma",
			text: "Который я решил. ",
			ctx:  Context{HasContext: true, Preceding: ','},
			want: " который я решил",
		},
		{
			name: "mid-sentence strips three-dot ellipsis",
			text: "Который я решил... ",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " который я решил",
		},
		{
			name: "mid-sentence strips unicode ellipsis",
			text: "Который я решил… ",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " который я решил",
		},
		{
			name: "mid-sentence strips exclamation+question combo",
			text: "Который я решил?! ",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " который я решил",
		},
		{
			name: "cursor on a letter: word follows, so exactly one trailing space appears",
			text: "Вставка. ",
			ctx:  Context{HasContext: true, Preceding: 'а', RightKnown: true, Following: 'с'},
			want: " вставка ",
		},
		{
			name: "cursor before existing space: no trailing space added",
			text: "Вставка. ",
			ctx:  Context{HasContext: true, Preceding: 'а', RightKnown: true, Following: ' '},
			want: " вставка",
		},
		{
			name: "cursor right before comma: bare tail",
			text: "Вставка. ",
			ctx:  Context{HasContext: true, Preceding: 'а', RightKnown: true, Following: ','},
			want: " вставка",
		},
		{
			// Caret at the very end of the field after a letter (e.g. the
			// sentence's own period was deleted and text is appended): lower-case
			// and lead-space like a mid-sentence insert, but KEEP the phrase's
			// terminator — it now ends the sentence — dropping only the blank.
			name: "mid-sentence at end of field keeps terminator",
			text: "Вставка. ",
			ctx:  Context{HasContext: true, Preceding: 'а', RightKnown: true, AtEnd: true},
			want: " вставка.",
		},
		{
			name: "after comma at end of field keeps terminator",
			text: "Можно сказать. ",
			ctx:  Context{HasContext: true, Preceding: ',', RightKnown: true, AtEnd: true},
			want: " можно сказать.",
		},
		{
			name: "exclamation at end of field kept (not stripped)",
			text: "Вот это да! ",
			ctx:  Context{HasContext: true, Preceding: 'а', RightKnown: true, AtEnd: true},
			want: " вот это да!",
		},
		{
			name: "space already left of caret, word further left: lowercase, no extra space",
			text: "Вставка. ",
			ctx:  Context{HasContext: true, Preceding: 'а', SpaceBefore: true, RightKnown: true, Following: 'с'},
			want: "вставка ",
		},
		{
			name: "space left of caret after sentence end: capitalise, no extra space",
			text: "привет. ",
			ctx:  Context{HasContext: true, Preceding: '.', SpaceBefore: true, RightKnown: true, Following: 'С'},
			want: "Привет. ",
		},

		// --- ambiguous / unknown left context ---
		{
			name: "whitespace-only left context is a fresh start (capitalise, keep punctuation)",
			text: "привет.",
			ctx:  Context{HasContext: true, Preceding: ' '},
			want: "Привет.",
		},
		{
			name: "opening bracket is mid-sentence, no leading space",
			text: "Привет.",
			ctx:  Context{HasContext: true, Preceding: '('},
			want: "привет",
		},
		{
			name: "em dash continues the clause",
			text: "Привет.",
			ctx:  Context{HasContext: true, Preceding: '—', SpaceBefore: true},
			want: "привет",
		},
		{
			name: "plain hyphen-minus continues the clause (lower-case)",
			text: "Пример.",
			ctx:  Context{HasContext: true, Preceding: '-', SpaceBefore: true},
			want: "пример",
		},
		{
			name: "opening guillemet quote is mid-sentence, no leading space",
			text: "Пример.",
			ctx:  Context{HasContext: true, Preceding: '«'},
			want: "пример",
		},

		// --- guards ---
		{
			name: "already-lowercase first letter untouched mid-sentence",
			text: "который я решил. ",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " который я решил",
		},
		{
			name: "already-capital first letter untouched at start",
			text: "Привет. ",
			ctx:  Context{HasContext: true, AtStart: true},
			want: "Привет. ",
		},
		{
			name: "non-letter first char: spacing rules only",
			text: "100 рублей. ",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " 100 рублей",
		},
		{
			name: "punctuation-only transcription survives mid-sentence strip",
			text: "?!",
			ctx:  Context{HasContext: true, Preceding: 'а'},
			want: " ?!",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(tc.text, tc.ctx)
			if got != tc.want {
				t.Fatalf("Apply(%q, %+v) = %q, want %q", tc.text, tc.ctx, got, tc.want)
			}
		})
	}
}
