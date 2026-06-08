package ruprofane

import "testing"

func TestDisabledIsPassthrough(t *testing.T) {
	SetEnabled(false)
	in := "ты блядь молодец"
	if got := Filter(in); got != in {
		t.Fatalf("disabled filter must not change text: %q", got)
	}
}

func TestMasksKeepingFirstLetterAndPunctuation(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	cases := map[string]string{
		"ты блядь молодец": "ты б•••• молодец",
		"Блядь!":           "Б••••!",
		"вот это пиздец":   "вот это п•••••",
		"ёбаный свет":      "ё••••• свет", // ё-folded to ебаный, which is listed
	}
	for in, want := range cases {
		if got := Filter(in); got != want {
			t.Errorf("Filter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoFalsePositives(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	// Innocent words: either share an obscene substring (рубля/сабля/корабля),
	// or are explicit exceptions (страхуй/мандат). Whole-word matching must
	// leave them untouched.
	clean := []string{
		"пять рублей за рубля",
		"острая сабля и корабля",
		"стебля растения",
		"страхуй машину и мандат депутата",
		"парикмахер постриг",
	}
	for _, in := range clean {
		if got := Filter(in); got != in {
			t.Errorf("false positive: Filter(%q) = %q", in, got)
		}
	}
}
