// Package crossjudge compares two transcription candidates from DIFFERENT
// engines (best Whisper vs best Nemotron) on cheap, applied, cosmetic
// signals — no neural net, no external dictionary. It only decides which of
// the two reads as the cleaner decode, for the Ctrl+F11 "★" hint; insertion
// is still chosen per hotkey, not here.
package crossjudge

import (
	"strings"
	"unicode"
)

// knownTerms is a tiny whitelist of dev terms we expect in Latin. A variant
// that preserved e.g. "RabbitMQ" beats one that produced Cyrillic gibberish
// for the same word. Not a dictionary — just a cheap set membership.
var knownTerms = map[string]bool{
	"rabbitmq": true, "kafka": true, "kubernetes": true, "k8s": true,
	"docker": true, "postgresql": true, "postgres": true, "redis": true,
	"grpc": true, "rest": true, "graphql": true, "prometheus": true,
	"grafana": true, "nginx": true, "react": true, "typescript": true,
	"javascript": true, "nodejs": true, "elasticsearch": true, "clickhouse": true,
	"mongodb": true, "terraform": true, "ansible": true, "helm": true,
	"envoy": true, "istio": true, "jaeger": true, "opentelemetry": true,
	"websocket": true, "json": true, "yaml": true, "kotlin": true, "golang": true,
}

// Score rates text's cosmetic quality (higher = cleaner). other is the rival
// candidate's text, used only for the relative length check.
func Score(text, other string) float64 {
	t := strings.TrimSpace(text)
	words := strings.Fields(t)
	if len(words) == 0 {
		return -1e9
	}
	s := 0.0
	s += 3.0 * latinRatio(t)               // 1. runglish preserved (not Cyrillicized)
	s += 2.0 * punctDensity(t, len(words)) // 2. punctuation present/dense
	if hasUpper(t) {                       // 3. has capital letters
		s += 1.0
	}
	s -= 2.0 * float64(stutterRuns(words)) // 4. adjacent repeated words = stutter loop
	s -= 3.0 * junkRatio(words)            // 6. single-letter / non-letter junk tokens
	if h := knownTermHits(words); h > 0 {  // 7. recognised real terms (capped)
		if h > 4 {
			h = 4
		}
		s += 0.5 * float64(h)
	}
	// 5. length: a candidate far shorter than its rival likely dropped words.
	if o := len([]rune(strings.TrimSpace(other))); o > 0 {
		if float64(len([]rune(t)))/float64(o) < 0.6 {
			s -= 3.0
		}
	}
	return s
}

// Better reports whether a is at least as good as b (a wins ties).
func Better(a, b string) bool { return Score(a, b) >= Score(b, a) }

func latinRatio(text string) float64 {
	var latin, letters int
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Latin, r) {
			latin++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(latin) / float64(letters)
}

// punctDensity is sentence/clause punctuation per word, capped at 1.0 so a
// punctuation-spam candidate can't run away with the score.
func punctDensity(text string, words int) float64 {
	if words == 0 {
		return 0
	}
	n := strings.Count(text, ".") + strings.Count(text, ",") +
		strings.Count(text, "!") + strings.Count(text, "?") +
		strings.Count(text, ";") + strings.Count(text, ":")
	d := float64(n) / float64(words)
	if d > 1.0 {
		d = 1.0
	}
	return d
}

func hasUpper(text string) bool {
	for _, r := range text {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// stutterRuns counts adjacent duplicate words (case-insensitive) — the shape
// of an RNNT/Whisper stutter loop ("идём идём идём").
func stutterRuns(words []string) int {
	n := 0
	for i := 1; i < len(words); i++ {
		if strings.EqualFold(words[i], words[i-1]) {
			n++
		}
	}
	return n
}

// junkRatio is the fraction of tokens that are a single letter or contain no
// letters at all — a marker of garbled decode.
func junkRatio(words []string) float64 {
	if len(words) == 0 {
		return 0
	}
	junk := 0
	for _, w := range words {
		if isJunk(w) {
			junk++
		}
	}
	return float64(junk) / float64(len(words))
}

func isJunk(w string) bool {
	letters := 0
	for _, r := range w {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters == 0 || letters == 1
}

// knownTermHits counts words whose letters-only lowercase form is a known
// dev term (RabbitMQ, Kafka, …).
func knownTermHits(words []string) int {
	hits := 0
	for _, w := range words {
		if knownTerms[lettersLower(w)] {
			hits++
		}
	}
	return hits
}

func lettersLower(w string) string {
	var b strings.Builder
	for _, r := range w {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
