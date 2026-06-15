package uicontext

import "testing"

// Shift+F12 forced mid-sentence transform: decapitalise, one leading
// space, strip the phrase's terminator — no field reading.
func TestApplyForceMid(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"decapitalise + lead space + strip period", "Вставка в середину. ", " вставка в середину"},
		{"strip ellipsis", "Текст… ", " текст"},
		{"strip exclamation+question", "Да?! ", " да"},
		{"already lowercase stays", "слово. ", " слово"},
		{"digit start: no case change, lead space, strip", "100 рублей. ", " 100 рублей"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(tc.text, Context{HasContext: true, ForceMid: true})
			if got != tc.want {
				t.Fatalf("Apply(%q, ForceMid) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}

	// ForceMid must win even if other fields are set.
	if got := Apply("Текст. ", Context{HasContext: true, ForceMid: true, AtStart: true}); got != " текст" {
		t.Fatalf("ForceMid priority: got %q, want %q", got, " текст")
	}
}
