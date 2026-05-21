// Package transcriber wraps whisper.cpp for speech-to-text inference on GPU.
package transcriber

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

type Config struct {
	ModelPath     string
	Language      string // "" = auto-detect
	BeamSize      int
	InitialPrompt string
}

type Transcriber struct {
	model whisper.Model
	cfg   Config
	mu    sync.Mutex
}

// New loads the model into VRAM. Call once at program start.
func New(cfg Config) (*Transcriber, error) {
	m, err := whisper.New(cfg.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", cfg.ModelPath, err)
	}
	return &Transcriber{model: m, cfg: cfg}, nil
}

func (t *Transcriber) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.model.Close()
}

// Transcribe runs inference on a mono 16 kHz float32 PCM buffer and returns
// the recognized text.
func (t *Transcriber) Transcribe(pcm []float32) (string, error) {
	if len(pcm) == 0 {
		return "", nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	ctx, err := t.model.NewContext()
	if err != nil {
		return "", fmt.Errorf("new context: %w", err)
	}
	// whisper_full_default_params defaults language to "en", which makes the
	// model transcribe Russian speech as if it were English (lost punctuation,
	// pseudo-translation, mangled terms). An empty config means "auto-detect".
	lang := t.cfg.Language
	if lang == "" {
		lang = "auto"
	}
	if err := ctx.SetLanguage(lang); err != nil {
		return "", fmt.Errorf("set language %q: %w", lang, err)
	}
	if t.cfg.BeamSize > 0 {
		ctx.SetBeamSize(t.cfg.BeamSize)
	}
	if t.cfg.InitialPrompt != "" {
		ctx.SetInitialPrompt(t.cfg.InitialPrompt)
	}

	if err := ctx.Process(pcm, nil, nil, nil); err != nil {
		return "", fmt.Errorf("process: %w", err)
	}

	var segments []string
	for {
		seg, err := ctx.NextSegment()
		if err != nil {
			break
		}
		if text := strings.TrimSpace(seg.Text); text != "" {
			segments = append(segments, text)
		}
	}
	return formatSegments(segments), nil
}

func formatSegments(segments []string) string {
	// The whisper.cpp Go binding trims each segment, so preserve boundaries here.
	return addTerminalSentenceSpace(addSentenceSpacing(removeHallucinations(strings.Join(segments, " "))))
}

func removeHallucinations(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || isHallucinationOnly(text) {
		return ""
	}
	for _, pattern := range hallucinationPatterns {
		text = pattern.ReplaceAllString(text, " ")
	}
	text = removeHallucinationSentences(text)
	text = collapseRepeatedSentences(text)
	text = collapseRepeatedWords(text)
	text = strings.Join(strings.Fields(text), " ")
	if isHallucinationOnly(text) {
		return ""
	}
	return text
}

func isHallucinationOnly(text string) bool {
	key := normalizeHallucinationKey(text)
	if key == "" {
		return true
	}
	_, ok := hallucinationExactKeys[key]
	return ok
}

func removeHallucinationSentences(text string) string {
	chunks := sentenceChunks(text)
	if len(chunks) == 0 {
		return text
	}

	var sb strings.Builder
	removed := false
	for _, chunk := range chunks {
		if isHallucinationOnly(chunk) {
			removed = true
			continue
		}
		sb.WriteString(chunk)
	}
	if !removed {
		return text
	}
	return sb.String()
}

func sentenceChunks(text string) []string {
	runes := []rune(text)
	var chunks []string
	start := 0
	for i, r := range runes {
		if !isSentencePunctuation(r) {
			continue
		}
		end := i + 1
		for end < len(runes) && isSpace(runes[end]) {
			end++
		}
		chunks = append(chunks, string(runes[start:end]))
		start = end
	}
	if start < len(runes) {
		chunks = append(chunks, string(runes[start:]))
	}
	return chunks
}

func normalizeHallucinationKey(text string) string {
	var sb strings.Builder
	previousSpace := true
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			previousSpace = false
			continue
		}
		if !previousSpace {
			sb.WriteRune(' ')
			previousSpace = true
		}
	}
	return strings.TrimSpace(sb.String())
}

func addSentenceSpacing(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) < 3 {
		return string(runes)
	}

	var sb strings.Builder
	for i, r := range runes {
		sb.WriteRune(r)
		if needsSpaceAfter(runes, i) {
			sb.WriteRune(' ')
		}
	}
	return sb.String()
}

func collapseRepeatedSentences(text string) string {
	chunks := sentenceChunks(text)
	if len(chunks) < 3 {
		return text
	}

	type run struct {
		start int
		key   string
		count int
	}

	runs := make([]run, 0)
	for i := 0; i < len(chunks); {
		key := normalizeHallucinationKey(chunks[i])
		j := i + 1
		for j < len(chunks) && key != "" && normalizeHallucinationKey(chunks[j]) == key {
			j++
		}
		runs = append(runs, run{start: i, key: key, count: j - i})
		i = j
	}

	changed := false
	var sb strings.Builder
	for _, r := range runs {
		if r.count >= 3 && isRepeatableHallucinationKey(r.key) {
			sb.WriteString(chunks[r.start])
			changed = true
			continue
		}
		for i := 0; i < r.count; i++ {
			sb.WriteString(chunks[r.start+i])
		}
	}
	if !changed {
		return text
	}
	return sb.String()
}

func isRepeatableHallucinationKey(key string) bool {
	return key != ""
}

func collapseRepeatedWords(text string) string {
	words := strings.Fields(text)
	if len(words) < 3 {
		return text
	}

	changed := false
	out := make([]string, 0, len(words))
	for i := 0; i < len(words); {
		key := normalizeHallucinationKey(words[i])
		j := i + 1
		for j < len(words) && normalizeHallucinationKey(words[j]) == key {
			j++
		}
		count := j - i
		if count >= 3 && isRepeatableWordKey(key) {
			out = append(out, words[i])
			changed = true
		} else {
			out = append(out, words[i:j]...)
		}
		i = j
	}
	if !changed {
		return text
	}
	return strings.Join(out, " ")
}

func isRepeatableWordKey(key string) bool {
	return key != ""
}

func needsSpaceAfter(runes []rune, i int) bool {
	if !isSentencePunctuation(runes[i]) || i+1 >= len(runes) {
		return false
	}
	if isSpace(runes[i+1]) || isSentencePunctuation(runes[i+1]) {
		return false
	}
	if runes[i] == '.' && i > 0 && isDigit(runes[i-1]) && isDigit(runes[i+1]) {
		return false
	}
	if runes[i] == '.' && i > 0 && isSingleLetterAbbreviation(runes, i) && unicode.IsLower(runes[i+1]) {
		return false
	}
	if i == 0 {
		return unicode.IsUpper(runes[i+1])
	}
	return unicode.IsUpper(runes[i+1]) || isCyrillic(runes[i-1]) && isCyrillic(runes[i+1])
}

func addTerminalSentenceSpace(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return ""
	}
	if isSentencePunctuation(runes[len(runes)-1]) {
		return string(runes) + " "
	}
	return string(runes)
}

func isSentencePunctuation(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}

func isSpace(r rune) bool {
	return unicode.IsSpace(r)
}

func isDigit(r rune) bool {
	return unicode.IsDigit(r)
}

func isSingleLetterAbbreviation(runes []rune, dot int) bool {
	if dot == 0 || !isLetter(runes[dot-1]) || !isLetter(runes[dot+1]) {
		return false
	}
	tokenStart := dot - 1
	for tokenStart > 0 && isLetter(runes[tokenStart-1]) {
		tokenStart--
	}
	return dot-tokenStart == 1
}

func isLetter(r rune) bool {
	return unicode.IsLetter(r)
}

func isCyrillic(r rune) bool {
	return unicode.In(r, unicode.Cyrillic)
}
