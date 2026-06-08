package ruprofane

import "testing"

func TestDisabledIsPassthrough(t *testing.T) {
	SetEnabled(false)
	in := "ты блядь молодец"
	if got := Filter(in); got != in {
		t.Fatalf("disabled filter must not change text: %q", got)
	}
}

func TestMasksMatKeepingFirstLetter(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	cases := map[string]string{
		"ты блядь молодец":  "ты б•••• молодец",
		"Блядь!":            "Б••••!",
		"вот это пиздец":    "вот это п•••••",
		"ёбаный свет":       "ё••••• свет",
		"полная хуйня":      "полная х••••",
		"да охуеть":         "да о•••••",
		"иди нахуй":         "иди н••••",
		"какой же он мудак": "какой же он м••••",
		"заебал ты":         "з••••• ты",
		"разъебать всё":     "р•••••••• всё",
		"долбоёб":           "д••••••",
		"да мне похуй":      "да мне п••••",
	}
	for in, want := range cases {
		if got := Filter(in); got != want {
			t.Errorf("Filter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNonMatLeftAlone(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	// Rude but NOT mat — must stay untouched per "только жёсткий мат".
	for _, in := range []string{"ты лох", "вот чмо", "жид", "пидор", "гондон", "сука", "жопа", "говно"} {
		if got := Filter(in); got != in {
			t.Errorf("non-mat must be left alone: Filter(%q) = %q", in, got)
		}
	}
}

func TestNoFalsePositives(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	clean := []string{
		"наша команда и командир",
		"мандарин и мандат депутата",
		"страхуй машину, застрахуй дом",
		"хлеб, погреб и требования",
		"ребёнок и ребята",
		"мебель в эпоху перемен",
		"рубля за саблю с корабля",
		"лохматый пёс и переполох",
		"мудрый изумруд",
	}
	for _, in := range clean {
		if got := Filter(in); got != in {
			t.Errorf("false positive: Filter(%q) = %q", in, got)
		}
	}
}
