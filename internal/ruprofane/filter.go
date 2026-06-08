// Package ruprofane roughly censors HARD Russian mat (мат) in text — the
// хуй / пизда / ебать / блядь families plus the genital mat (муде / манда /
// залупа / елда). General ругательства that are NOT mat (лох, жид, чмо,
// пидор, гондон, сука, …) are deliberately left alone, and there is no Latin
// / English filtering — both by explicit request.
//
// It is a VIEW transform, applied only at display (picker plaques + the
// recent-phrase tray menu) and at insertion, gated by a runtime toggle (tray
// "Фильтр мата", on by default). The recognized text is always stored and
// scored uncensored, so a false positive never destroys the phrase: untick
// the box and the original reappears.
//
// Matching is by ROOT (substring on a ё-folded, lowercased word), so every
// derivative and compound of a mat family is caught (охуеть, пиздец,
// разъебать, долбоёб, …) without listing each form. The roots are taken from
// the user's darkforest/shoot2d username moderator, narrowed to strict mat.
// The еб-family uses word-initial + prefixed forms only (заеб/наеб/выеб/…),
// never the bare "еб", so хлеб / погреб / требовать / ребёнок are safe.
// exceptions_ru.txt whitelists the few remaining collisions (команда←манда,
// мандарин/мандат, страхуй←хуй). Matched words are masked keeping the first
// letter, with bullets (блядь → б••••).
package ruprofane

import (
	_ "embed"
	"regexp"
	"strings"
	"sync/atomic"
)

//go:embed exceptions_ru.txt
var exceptionsRaw string

var (
	wordRe = regexp.MustCompile(`[\p{L}]+`)
	// хуй family: х + у + vowel/й. "хунта", "пастуху", "ночую" don't match.
	huyRe          = regexp.MustCompile(`ху[йяеиюя]`)
	enabled        atomic.Bool
	exceptionStems = parseExceptions(exceptionsRaw)
)

// ebPrefixed — ебать-family with an explicit prefix (substring-safe: no clean
// word contains these). Bare "еб" is handled as a word-initial prefix only.
var ebPrefixed = []string{
	"заеб", "наеб", "выеб", "проеб", "доеб", "уеб", "объеб", "разъеб",
	"подъеб", "съеб", "въеб", "перееб", "поеб", "отъеб", "изъеб", "недоеб",
}

// ebContains — ебать-family roots safe to match anywhere in a word.
var ebContains = []string{"долбоеб", "далбаеб", "ебл", "ебан", "ебуч", "ебар"}

// genitalMat — the genital-mat roots (муде/манда/залупа/елда families).
var genitalMat = []string{"залуп", "елда", "мандавошк", "манда", "муде", "мудак", "мудил", "мудоз"}

// slurRoots — the пидор/педик family. Not one of the classic mat roots, but
// added on request (catches пидармотина, пидорас, пидрила). "педик" can graze
// велосипедик/мопедик — guarded by exceptions_ru.txt.
var slurRoots = []string{"пидор", "пидар", "пидр", "педик", "педрил", "педераст"}

// maskChar is the bullet (U+2022), used instead of '*' so the mask never
// collides with asterisk markup or other meaningful punctuation.
const maskChar = "•"

// SetEnabled turns the filter on/off at runtime (tray checkbox).
func SetEnabled(on bool) { enabled.Store(on) }

// Enabled reports the current state (for menu rendering / persistence).
func Enabled() bool { return enabled.Load() }

// Filter masks hard mat per word when enabled; returns text unchanged when
// disabled. Preserves punctuation and spacing.
func Filter(text string) string {
	if !enabled.Load() {
		return text
	}
	return wordRe.ReplaceAllStringFunc(text, func(w string) string {
		if !isException(w) && isMat(w) {
			return maskWord(w)
		}
		return w
	})
}

// isMat reports whether a single word is hard mat (or a derivative).
func isMat(word string) bool {
	w := foldYo(strings.ToLower(word))
	if huyRe.MatchString(w) {
		return true
	}
	if strings.Contains(w, "пизд") || strings.Contains(w, "пезд") {
		return true
	}
	if strings.Contains(w, "бляд") || strings.Contains(w, "блят") || w == "бля" {
		return true
	}
	for _, r := range genitalMat {
		if strings.Contains(w, r) {
			return true
		}
	}
	for _, r := range slurRoots {
		if strings.Contains(w, r) {
			return true
		}
	}
	if strings.HasPrefix(w, "еб") { // word starts еб/ёб — only mat does
		return true
	}
	for _, p := range ebPrefixed {
		if strings.Contains(w, p) {
			return true
		}
	}
	for _, r := range ebContains {
		if strings.Contains(w, r) {
			return true
		}
	}
	return false
}

// isException spares a word whose folded form contains a whitelisted stem.
func isException(w string) bool {
	key := foldYo(strings.ToLower(w))
	for _, stem := range exceptionStems {
		if strings.Contains(key, stem) {
			return true
		}
	}
	return false
}

// maskWord keeps the first rune and replaces the rest with bullets.
func maskWord(w string) string {
	r := []rune(w)
	if len(r) <= 1 {
		return w
	}
	return string(r[0]) + strings.Repeat(maskChar, len(r)-1)
}

// foldYo maps ё→е so spelling variants collapse.
func foldYo(s string) string { return strings.ReplaceAll(s, "ё", "е") }

func parseExceptions(data string) []string {
	var out []string
	for _, line := range strings.Split(data, "\n") {
		s := foldYo(strings.ToLower(strings.TrimSpace(line)))
		if i := strings.IndexByte(s, '#'); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
