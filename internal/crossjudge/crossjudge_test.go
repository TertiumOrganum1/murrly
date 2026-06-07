package crossjudge

import "testing"

func TestLatinPreservedBeatsCyrillicized(t *testing.T) {
	good := "Поднял RabbitMQ и Kafka, всё работает."
	bad := "Поднял реббит мкью и кафко, всё работает."
	if !Better(good, bad) {
		t.Fatalf("Latin-preserving (%.2f) should beat Cyrillicized (%.2f)", Score(good, bad), Score(bad, good))
	}
}

func TestPunctuatedCapitalisedBeatsBare(t *testing.T) {
	good := "Короче, смотри, это работает. Дальше проверим."
	bare := "короче смотри это работает дальше проверим"
	if !Better(good, bare) {
		t.Fatalf("clean (%.2f) should beat bare (%.2f)", Score(good, bare), Score(bare, good))
	}
}

func TestStutterPenalised(t *testing.T) {
	clean := "Дежурный инженер идёт смотреть логи."
	stutter := "Идём идём идём смотреть смотреть логи."
	if Score(stutter, clean) >= Score(clean, stutter) {
		t.Fatalf("stutter should score below clean")
	}
}

func TestMuchShorterPenalised(t *testing.T) {
	full := "Слушай, надо проверить весь список терминов в документе."
	truncated := "Слушай."
	if Better(truncated, full) {
		t.Fatalf("truncated should not beat the full candidate")
	}
}
