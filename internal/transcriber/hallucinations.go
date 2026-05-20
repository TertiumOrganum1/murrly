package transcriber

import (
	_ "embed"
	"encoding/csv"
	"regexp"
	"strconv"
	"strings"
)

//go:embed hallucinations.csv
var hallucinationsCSV string

var hallucinationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*в\s+этом\s+видео\s+я\s+покажу[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*всем\s+привет\s+и\s+добро\s+пожаловать\s+на\s+(?:мой|наш)\s+канал[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:смотрите\s+)?продолжение\s+(?:следует|в\s+\d+\s+части)[.!?…]*`),
	regexp.MustCompile(`(?i)\s*смотрите\s+(?:другие\s+видео|следующее\s+видео)[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:канал\s+)?субтитры\s+(?:сделал[аи]?|сделаны|делал[аи]?|создавал[аи]?|создал[аи]?|предоставил[аи]?|добавил[аи]?)(?:\s+\S+){0,6}[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:сделал|делал|добавил|создавал)\s+dimatorzok[.!?…]*`),
	regexp.MustCompile(`(?i)\s*редактор\s+субтитров(?:\s+\S+){0,8}[.!?…]*`),
	regexp.MustCompile(`(?i)\s*корректор(?:\s+\S+){1,6}[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:спасибо\s+за\s+(?:просмотр|внимание|субтитры)|подписывайтесь\s+на\s+(?:мой|наш)\s+канал)[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:ставьте\s+лайки|не\s+забудьте\s+подписаться|подпишитесь\s+на\s+канал|подпишись\s+на\s+канал|подписывайтесь\s+на\s+канал)[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:thanks|thank\s+you)\s+for\s+watching[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:please\s+subscribe\s+to\s+my\s+channel|like\s+and\s+subscribe|don'?t\s+forget\s+to\s+subscribe|subscribe\s+to\s+my\s+channel)[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*subtitles\s+by\s+the\s+amara[.\s]*org\s+community[.!?…]*`),
	regexp.MustCompile(`(?i)\s*(?:subtitles?|captions?|captioned|closed\s+captioning)\s+(?:by|provided\s+by|created\s+by)[^.!?…]*[.!?…]*`),
	regexp.MustCompile(`(?i)\s*transcri(?:bed|ption)\s+by[^.!?…]*[.!?…]*`),
}

var hallucinationExactKeys = loadHallucinationExactKeys()

var manualHallucinationPhrases = []string{
	"ну и конечно это не все",
	"продолжение следует",
	"это все что я могу сказать",
	"спасибо за просмотр",
	"спасибо за внимание",
	"подписывайтесь на мой канал",
	"подписывайтесь на наш канал",
	"subtitles by the amara org community",
	"thank you for watching",
	"thank you for watching please subscribe",
	"thanks for watching",
	"thanks for watching please subscribe",
}

var strongHallucinationFragments = []string{
	"amara org",
	"captioned by",
	"captions by",
	"closed captioning",
	"don t forget to subscribe",
	"like and subscribe",
	"please subscribe",
	"satsang with mooji",
	"subscribe to my channel",
	"subtitles",
	"thank you for watching",
	"thanks for watching",
	"transcribed by",
	"transcription by",
	"корректор",
	"не забудьте подписаться",
	"подпишись",
	"подпишитесь",
	"подписывайтесь",
	"продолжение следует",
	"редактор субтитров",
	"спасибо за внимание",
	"спасибо за просмотр",
	"ставьте лайки",
	"субтитр",
}

func loadHallucinationExactKeys() map[string]struct{} {
	keys := make(map[string]struct{}, 512)
	for _, phrase := range manualHallucinationPhrases {
		addHallucinationKey(keys, phrase)
	}

	r := csv.NewReader(strings.NewReader(hallucinationsCSV))
	r.FieldsPerRecord = 3
	records, err := r.ReadAll()
	if err != nil {
		return keys
	}
	for i, record := range records {
		if i == 0 || len(record) != 3 {
			continue
		}
		lang := record[0]
		if lang != "en" && lang != "ru" {
			continue
		}
		count, _ := strconv.Atoi(record[2])
		key := normalizeHallucinationKey(record[1])
		if shouldUseDatasetHallucination(lang, key, count) {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func addHallucinationKey(keys map[string]struct{}, phrase string) {
	key := normalizeHallucinationKey(phrase)
	if key != "" {
		keys[key] = struct{}{}
	}
}

func shouldUseDatasetHallucination(lang, key string, count int) bool {
	if key == "" {
		return false
	}
	if hasStrongHallucinationMarker(key) {
		return true
	}
	words := strings.Fields(key)
	if lang == "ru" {
		return true
	}
	if lang == "en" {
		return len(words) >= 5 && len([]rune(key)) >= 28
	}
	if count >= 2 && len(words) >= 3 && len([]rune(key)) >= 12 {
		return true
	}
	return len(words) >= 5 && len([]rune(key)) >= 24
}

func hasStrongHallucinationMarker(key string) bool {
	for _, fragment := range strongHallucinationFragments {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}
