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
	// addSentenceSpacing inserts the missing spaces; the new
	// capitalizeAfter… pass then promotes leading letters after every
	// terminator (and the very first letter of the text).
	want := "Первое предложение. Второе? Третье! Четвертое. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsKeepsDecimalsAndAbbreviations(t *testing.T) {
	got := formatSegments([]string{"Версия 3.14 работает т.е. корректно"})
	want := "Версия 3.14 работает т.е. корректно. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsKeepsTechnicalDots(t *testing.T) {
	got := formatSegments([]string{"github.com, README.md и Node.js не меняются"})
	want := "github.com, README.md и Node.js не меняются. "
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

// Whisper sometimes drops the trailing terminator; we now always
// append one so paste-time text reads as a finished thought.
func TestFormatSegmentsAddsTerminalPeriodToUnfinishedSentence(t *testing.T) {
	got := formatSegments([]string{"Готово"})
	want := "Готово. "
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

// The "вовремя/сделать это" sentence is a Whisper stutter loop the
// user has hit repeatedly — both with and without the leading "Но".
// Both variants are now in the CSV and should be dropped outright.
func TestFormatSegmentsDropsTimingLoopHallucination(t *testing.T) {
	tests := []string{
		"Но если ты хочешь, чтобы это было вовремя, то ты можешь сделать это.",
		"Если ты хочешь, чтобы это было вовремя, то ты можешь сделать это.",
	}
	for _, input := range tests {
		if got := formatSegments([]string{input}); got != "" {
			t.Fatalf("formatSegments(%q) = %q, want empty", input, got)
		}
	}
}

func TestFormatSegmentsCollapsesRepeatedSentenceBlock(t *testing.T) {
	got := formatSegments([]string{
		"Проверь сборку. Дальше надо смотреть логи. Дальше надо смотреть логи. Дальше надо смотреть логи. Дальше надо смотреть логи.",
	})
	want := "Проверь сборку. Дальше надо смотреть логи. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = formatSegments([]string{
		"Начало. Дальше надо смотреть логи. Дальше надо смотреть логи. Потом проверить сервис.",
	})
	want = "Начало. Дальше надо смотреть логи. Потом проверить сервис. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Block-repeat must fire at 2+ copies, not the old 3+ threshold.
// A repeated arbitrary phrase that doesn't align with sentence
// boundaries still needs to collapse.
func TestFormatSegmentsCollapsesArbitraryBlocksAtTwoCopies(t *testing.T) {
	got := formatSegments([]string{
		"Сборка прошла. Я не знаю что это такое я не знаю что это такое.",
	})
	want := "Сборка прошла. Я не знаю что это такое. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Mid-phrase comma differences shouldn't block dedup — the second
// copy missing a comma is the user's classic example.
func TestFormatSegmentsCollapsesIgnoringPunctuationDifferences(t *testing.T) {
	got := formatSegments([]string{
		"вот моя фраза, которую я говорю, вот моя фраза которую я говорю",
	})
	// Block-collapse keeps the first copy as-is; capitalizeAfter…
	// then promotes the leading lowercase "в" because it's the very
	// first letter of the whole text.
	want := "Вот моя фраза, которую я говорю. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSegmentsCollapsesRepeatedSingleWords(t *testing.T) {
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

// Two passes resolve nested repeats: outer block collapses on pass 1,
// the inner block inside the kept copy collapses on pass 2.
func TestFormatSegmentsCollapsesNestedRepeatsInTwoPasses(t *testing.T) {
	got := formatSegments([]string{"А Б А Б В А Б А Б В"})
	want := "А Б В. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Integration test: feed the verbatim Whisper raw output from the
// user's actual stutter case (murrly.log 2026/05/24 01:01:58) and
// assert the pipeline produces a sane result — one copy of the long
// repeated sentence, plus the unique tail. This exercises the
// interaction of two filters: collapseRepeatedBlocks (catches the
// consecutive runs AND folds an A_double chunk back into a single
// A by collapsing its internal "В общем суть в том ..." duplication)
// and dedupeSubstantialSentences (any A copy that survives the
// consecutive-block pass). The leading "В общем," is kept now — "в общем"
// is ambiguous filler, removed only as a standalone sentence, not a clause.
func TestFormatSegmentsHandlesRealStutterFromLog(t *testing.T) {
	raw := "Слушай, я диктовал текст, вот последний мой, ну предпоследний получается, учитывая эту реплику, и как будто бы очень много в конце было удалено, ну то есть как будто бы, я не знаю. " +
		"В общем, посмотри влоги, предпоследнюю реплику, приведи мне сюда ее, ну или знаешь, скажи мне, где лог находится, я сам гляну. " +
		// 7 consecutive A's
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		// A_double — Whisper inserted a fragment between copies without a period
		"В общем, суть в том, что надо выяснить, это В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		// 6 more A's
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		// A_double again
		"В общем, суть в том, что надо выяснить, это В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		// 6 more A's
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
		// unique final
		"В общем, суть в том, что надо выяснить, это которые я не говорил, и потом того, что я говорил, дальше нет."
	got := formatSegments([]string{raw})
	fullCount := strings.Count(got, "наши фильтры так все урезают, что в итоге я не вижу.")
	if fullCount != 1 {
		t.Errorf("expected exactly 1 copy of the long A sentence, got %d:\n%s", fullCount, got)
	}
	if !strings.Contains(got, "которые я не говорил, и потом того, что я говорил, дальше нет.") {
		t.Errorf("missing unique final sentence: %s", got)
	}
}

// Whisper sometimes loops on a long sentence with short fragments
// interleaved between copies — that breaks consecutive-block
// detection but dedupeSubstantialSentences catches the dupes by
// normalised form across the whole text.
func TestFormatSegmentsDedupesNonConsecutiveLongRepeats(t *testing.T) {
	got := formatSegments([]string{
		"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
			"В общем, суть в том, что надо выяснить, это В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
			"В общем, суть в том, что надо выяснить, это наши фильтры так все урезают, что в итоге я не вижу. " +
			"В общем, суть в том, что надо выяснить, это которые я не говорил.",
	})
	// The long "наши фильтры…" sentence appears 3 times by normalised
	// form; we keep only the first. The fragments still pass through
	// because they're below the minimum-word threshold OR uniquely
	// shaped.
	if strings.Count(got, "наши фильтры так все урезают, что в итоге я не вижу.") > 1 {
		t.Fatalf("expected long sentence collapsed to one copy, got: %q", got)
	}
}

// Short sentences (under the dedupeSubstantialMinWords threshold)
// can recur naturally — don't aggressively dedupe.
func TestFormatSegmentsKeepsShortRepeatingSentences(t *testing.T) {
	got := formatSegments([]string{"Понял всё. Сделаю. Понял всё. Уже работаю."})
	want := "Понял всё. Сделаю. Понял всё. Уже работаю. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Whisper occasionally emits a stray single-word filler sentence
// between real ones ("кратенько. значит. подведем"). The filler
// "значит." is removed by stripFillerOnlySentences; the lowercase
// "подведем" promoted into sentence-initial position is then
// capitalised. End result reads as two clean sentences instead of
// one ungrammatical run-on.
func TestFormatSegmentsHandlesStrayMidSentenceFiller(t *testing.T) {
	got := formatSegments([]string{"Давай еще кратенько. значит. подведем итог задачи."})
	want := "Давай еще кратенько. Подведем итог задачи. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Single-letter abbreviations are protected even though their final
// period is followed by a lowercase letter ("т.е. корректно"). The
// very-first-letter rule still capitalises "работает".
func TestFormatSegmentsKeepsSingleLetterAbbreviation(t *testing.T) {
	got := formatSegments([]string{"работает т.е. корректно"})
	want := "Работает т.е. корректно. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Three or more single-word "sentences" in a row are over-fragmented
// by Whisper — merge them and keep only the trailing terminator.
func TestFormatSegmentsJoinsSingleWordSentenceRuns(t *testing.T) {
	got := formatSegments([]string{"Раз. Два. Три. Четыре."})
	want := "Раз Два Три Четыре. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Two single-word sentences are legitimate ("Yes. No.") — left alone.
func TestFormatSegmentsKeepsPairOfSingleWordSentences(t *testing.T) {
	got := formatSegments([]string{"Да. Нет."})
	want := "Да. Нет. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Standalone discourse-marker sentences ("Вот.", "Так.") are pure
// filler and get removed. "Поэтому." is NOT a filler and stays.
func TestFormatSegmentsRemovesStandaloneFillerSentences(t *testing.T) {
	got := formatSegments([]string{"Поэтому. Так. Давай еще кратенько значит подведем итог."})
	want := "Поэтому. Давай еще кратенько значит подведем итог. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Multi-token all-filler sentences are also removed ("Ну как бы.",
// "В общем."). Single tokens inside real sentences must not be
// touched.
func TestFormatSegmentsRemovesMultiTokenFillerSentences(t *testing.T) {
	got := formatSegments([]string{"Так. Ну как бы. В общем. Дело сделано."})
	want := "Дело сделано. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Filler words inside a real sentence are content, not noise — leave
// them be.
func TestFormatSegmentsKeepsFillerWordsInsideSentences(t *testing.T) {
	got := formatSegments([]string{"Это вот так получилось."})
	want := "Это вот так получилось. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// After block-collapse the first copy may end in a comma — strip
// trailing ,/;/: before adding the terminal period.
func TestFormatSegmentsStripsTrailingComma(t *testing.T) {
	got := formatSegments([]string{"Я говорил это, я говорил это,"})
	want := "Я говорил это. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Ellipsis (the "…" rune or any run of 2+ dots) is always a Whisper
// pause artifact, never authored punctuation in the user's text.
// "…" / "..." / ".." all become a single "." — the sentence
// boundary Whisper saw is preserved as a normal period, so the
// already-capitalised next word reads naturally as a new sentence.
func TestFormatSegmentsNormalisesEllipsisToPeriod(t *testing.T) {
	tests := map[string]string{
		// Trailing fragments after an ellipsis pause read as new
		// sentences; capitalizeAfter… promotes the lowercase post-
		// pause word.
		"Подожди... сейчас гляну.":   "Подожди. Сейчас гляну. ",
		"Подумал.. потом передумал.": "Подумал. Потом передумал. ",
		// Lone trailing run collapses to a single period; finalize…
		// is happy.
		"Готово……": "Готово. ",
		// Whisper's already-capitalised post-pause word stays
		// capitalised (the post-period sentence boundary is real),
		// stripFillerBetweenCommas removes "короче,".
		"Ну а под капотом там уже этот... Чё надо делать, короче, с ямлом?": "Ну а под капотом там уже этот. Чё надо делать, с ямлом? ",
		// Abbreviation directly followed by ellipsis must not
		// produce a double period, and the following word is NOT
		// capitalised because the preceding "д." is a single-letter
		// abbreviation (not a real sentence end).
		"См. т.д... корректно": "См. т.д. корректно. ",
	}
	for input, want := range tests {
		if got := formatSegments([]string{input}); got != want {
			t.Errorf("formatSegments(%q) = %q, want %q", input, got, want)
		}
	}
}

// Filler clauses between commas are noise — drop them with their
// commas. Position-insensitive: start, middle, end. When the leading
// filler is dropped, capitalizeAfter… promotes the new first word.
func TestFormatSegmentsStripsFillerClausesBetweenCommas(t *testing.T) {
	tests := map[string]string{
		// "типа" is always-filler (dropped); "в общем" is ambiguous and
		// is kept as a comma clause inside a real sentence.
		"Поэтому, типа, в общем, давай поспеши.":  "Поэтому, в общем, давай поспеши. ",
		"Типа, давай поспеши.":                    "Давай поспеши. ",
		"Давай поспеши, типа.":                    "Давай поспеши. ",
		"Давай, ну, как бы, поспеши.":             "Давай, поспеши. ",
		// Sentence that's purely filler clauses gets dropped wholesale.
		"Типа, как бы, в общем.": "",
	}
	for input, want := range tests {
		if got := formatSegments([]string{input}); got != want {
			t.Errorf("formatSegments(%q) = %q, want %q", input, got, want)
		}
	}
}

// Filler words inside content (no comma separating them) stay — they
// were always meaningful context, not parenthetical filler.
func TestFormatSegmentsKeepsFillerInsideClauseWithoutCommas(t *testing.T) {
	got := formatSegments([]string{"Я типа пошёл туда."})
	want := "Я типа пошёл туда. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Whisper sometimes drops the capital on the first word of a new
// sentence. We capitalise it.
func TestFormatSegmentsCapitalisesAfterPeriod(t *testing.T) {
	got := formatSegments([]string{"Это был день. потом я ушёл."})
	want := "Это был день. Потом я ушёл. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// stripFillerBetweenCommas can drop a leading filler clause and leave
// a lowercase word at sentence start ("какие-то. Короче, если..." →
// "какие-то. если..."). capitalizeAfterSentenceTerminators picks it up.
func TestFormatSegmentsCapitalisesAfterFillerClauseRemoval(t *testing.T) {
	got := formatSegments([]string{
		"Операции какие-то. Короче, если ты уверен, что это работает.",
	})
	want := "Операции какие-то. Если ты уверен, что это работает. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The very first letter of the whole text gets capitalised too,
// useful when the first sentence's filler was stripped.
func TestFormatSegmentsCapitalisesFirstLetter(t *testing.T) {
	got := formatSegments([]string{"Короче, попробовал, ничего не работает."})
	// "Короче," is an always-filler clause at sentence start, so it's
	// dropped, leaving "попробовал, …" lowercase, which we then capitalise.
	want := "Попробовал, ничего не работает. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Single-letter abbreviations are still safe — "т.е. корректно"
// must not become "т.е. Корректно".
func TestFormatSegmentsLeavesAbbreviationFollowingWordLowercase(t *testing.T) {
	got := formatSegments([]string{"работает т.е. корректно"})
	want := "Работает т.е. корректно. "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The real fast-mode signature: long enough to clear the word
// threshold, all lowercase, no sentence punctuation anywhere — but
// possibly with a single leading capital (which our own pipeline
// adds afterwards anyway).
func TestLooksLikeFastMode(t *testing.T) {
	tests := map[string]bool{
		// 15+ words, 0 internal punct, all lowercase → fast-mode.
		"да тут вообще вот насчёт того что ты говоришь он выводит типы точнее формат выводит да": true,
		// Our pipeline-added leading capital + terminal period must
		// NOT mask the fast-mode signal.
		"Да тут вообще вот насчёт того что ты говоришь он выводит типы точнее формат выводит да.": true,
		// Same idea, ? terminator at the end.
		"да тут вообще вот насчёт того что ты говоришь он выводит типы точнее формат выводит да?": true,
		// All lowercase including brand names — Whisper not capitalising
		// "GitHub" / "Docker" is part of the fast-mode signature.
		"да тут вообще про github и docker и kubernetes я не очень разбираюсь хочется конечно но времени нет": true,
		// Real text — multiple sentences, internal periods.
		"Это была сложная задача. Я её решил. Дальше что-то ещё хочу написать. Посмотрим что получится.": false,
		// Single-sentence real text with internal commas (15+ words).
		"Это была сложная задача, которую мне дали, и я её в итоге решил, потратив на это полдня и нервы.": false,
		// Internal uppercase from a proper noun disqualifies.
		"да тут вообще про GitHub и Docker я не очень разбираюсь хочется конечно но времени нет вообще": false,
		// Too short to trigger — Whisper sometimes legitimately emits
		// short uppercase-free fragments.
		"привет как дела": false,
	}
	for input, want := range tests {
		got := looksLikeFastMode(input)
		if got != want {
			t.Errorf("looksLikeFastMode(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestFormatSegmentsKeepsShortDatasetPhrases(t *testing.T) {
	// All dataset phrases below are below the threshold for being
	// treated as hallucinations, so they pass through and gain the
	// guaranteed trailing period plus a capitalised first letter.
	tests := map[string]string{
		"you":                                     "You. ",
		"bye":                                     "Bye. ",
		"good morning":                            "Good morning. ",
		"I am not sure if this is the right way.": "I am not sure if this is the right way. ",
	}
	for input, want := range tests {
		if got := formatSegments([]string{input}); got != want {
			t.Fatalf("formatSegments(%q) = %q, want %q", input, got, want)
		}
	}
}
