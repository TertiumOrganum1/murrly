// Package transcriber wraps whisper.cpp for speech-to-text inference on GPU.
package transcriber

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

type Config struct {
	ModelPath     string
	Language      string // "" = auto-detect
	BeamSize      int
	// BeamAdaptive: when true, beam dynamically scales with clip length —
	// short clips use 1 (effectively greedy), long clips bump to
	// longAudioBeamSize. When false, BeamSize is used unchanged for
	// every call.
	BeamAdaptive  bool
	InitialPrompt string
}

type Transcriber struct {
	model whisper.Model
	ctx   whisper.Context // reused across transcriptions to avoid per-call buffer allocation
	cfg   Config
	mu    sync.Mutex
}

const (
	// pcmSampleRateHz matches what the recorder feeds us (mono 16 kHz
	// float32 — Whisper's native sample rate).
	pcmSampleRateHz = 16000
	// longAudioThresholdSec — clips longer than this are "long" for
	// the purposes of adaptive beam scaling. Whisper's encoder window
	// is 30 s and greedy decode (beam_size=1) drops punctuation past
	// roughly this length.
	longAudioThresholdSec = 25.0
	// shortAudioBeamSize / longAudioBeamSize — beam values when
	// cfg.BeamAdaptive is true. Short clips use width 1 (effectively
	// greedy — fastest); long clips bump to 5 (upstream whisper-cli
	// default). 5 is what empirically restored punctuation on long
	// dictations — narrower widths (2-3) turned out insufficient.
	shortAudioBeamSize = 1
	longAudioBeamSize  = 5
)

// New loads the model into VRAM/GPU memory and allocates the inference
// context (KV cache + compute buffers, ~680MB for large-v3 turbo). Call
// once at program start. The context is reused for every Transcribe call —
// re-allocating Metal buffers on each push-to-talk adds ~1 second on M1 Pro,
// so caching it is the main latency win.
func New(cfg Config) (*Transcriber, error) {
	t0 := time.Now()
	m, err := whisper.New(cfg.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", cfg.ModelPath, err)
	}
	loadMs := time.Since(t0).Milliseconds()
	t1 := time.Now()
	ctx, err := m.NewContext()
	if err != nil {
		_ = m.Close()
		return nil, fmt.Errorf("new context: %w", err)
	}
	ctxMs := time.Since(t1).Milliseconds()
	log.Printf("transcriber: model=%s load=%dms ctx=%dms (total startup=%dms)", cfg.ModelPath, loadMs, ctxMs, loadMs+ctxMs)
	// Apply config settings once. Language can change per call if needed,
	// but BeamSize and InitialPrompt are stable.
	lang := cfg.Language
	if lang == "" {
		// whisper_full_default_params defaults language to "en", which makes
		// the model transcribe Russian speech as if it were English (lost
		// punctuation, pseudo-translation, mangled terms). "auto" lets
		// Whisper detect language from the input.
		lang = "auto"
	}
	if err := ctx.SetLanguage(lang); err != nil {
		_ = m.Close()
		return nil, fmt.Errorf("set language %q: %w", lang, err)
	}
	if cfg.BeamSize > 0 {
		ctx.SetBeamSize(cfg.BeamSize)
	}
	// Force deterministic decode and disable Whisper's temperature
	// fallback. Whisper retries each chunk at T=0.0, 0.2, 0.4, … 1.0
	// when its quality checks (entropy / logprob / compression ratio)
	// trip; high-temperature outputs are the ones that come back
	// lowercase with no punctuation. Pinning T=0 with beam_search keeps
	// the decoder in the only mode we want. -1 disables fallback per
	// the binding's documented contract.
	ctx.SetTemperature(0)
	ctx.SetTemperatureFallback(-1)
	if cfg.InitialPrompt != "" {
		ctx.SetInitialPrompt(cfg.InitialPrompt)
	}
	return &Transcriber{model: m, ctx: ctx, cfg: cfg}, nil
}

func (t *Transcriber) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.model.Close()
}

// Transcribe runs inference on a mono 16 kHz float32 PCM buffer and
// returns the recognized text. If the first pass produces what looks
// like Whisper's degraded fast-mode output (long enough but no
// punctuation, no uppercase), it retries up to fastModeMaxRetries
// times with progressively more leading silence prepended and a
// wider beam (longAudioBeamSize). Shifting the audio's chunk-boundary
// alignment plus widening the search both perturb the decoder off
// the fast-mode trajectory.
//
// Fast-mode detection runs on the RAW segments from whisper.cpp, not
// on the post-processed text — our pipeline always adds a leading
// capital and a trailing period, which would mask the signal if the
// detection looked at the final string.
func (t *Transcriber) Transcribe(pcm []float32) (string, error) {
	if len(pcm) == 0 {
		return "", nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	beam := t.chooseBeam(pcm)
	firstSegs, err := t.transcribeOnce(pcm, beam)
	if err != nil {
		return "", err
	}
	bestSegs := firstSegs
	if looksLikeFastMode(strings.Join(firstSegs, " ")) {
		silentSamples := int(fastModeSilencePadSec * pcmSampleRateHz)
		paddedPcm := pcm
		for attempt := 1; attempt <= fastModeMaxRetries; attempt++ {
			paddedPcm = append(make([]float32, silentSamples), paddedPcm...)
			totalSilence := fastModeSilencePadSec * float64(attempt)
			retryBeam := longAudioBeamSize
			firstRaw := strings.Join(firstSegs, " ")
			log.Printf("transcriber: fast-mode output (words=%d, no internal punct/caps); retry %d/%d with %.1fs leading silence and beam=%d",
				len(strings.Fields(firstRaw)), attempt, fastModeMaxRetries, totalSilence, retryBeam)
			retrySegs, err := t.transcribeOnce(paddedPcm, retryBeam)
			if err != nil {
				log.Printf("transcriber: retry %d failed: %v — keeping first result", attempt, err)
				break
			}
			if !looksLikeFastMode(strings.Join(retrySegs, " ")) {
				log.Printf("transcriber: retry %d recovered formatting", attempt)
				bestSegs = retrySegs
				break
			}
			if attempt == fastModeMaxRetries {
				log.Printf("transcriber: all %d retries still fast-mode; keeping first result", fastModeMaxRetries)
			}
		}
	}

	raw := strings.Join(bestSegs, " ")
	formatted := formatSegments(bestSegs)
	if raw != formatted {
		log.Printf("transcriber: raw=%q formatted=%q", raw, formatted)
	}
	return formatted, nil
}

// chooseBeam picks the beam_size for this clip based on configuration
// (static cfg.BeamSize, or BeamAdaptive's short/long split).
func (t *Transcriber) chooseBeam(pcm []float32) int {
	beam := t.cfg.BeamSize
	if beam < 1 {
		beam = 1
	}
	if t.cfg.BeamAdaptive {
		audioSec := float64(len(pcm)) / float64(pcmSampleRateHz)
		if audioSec > longAudioThresholdSec {
			beam = longAudioBeamSize
		} else {
			beam = shortAudioBeamSize
		}
	}
	return beam
}

// transcribeOnce runs whisper once with the given beam_size and
// returns the raw segments (caller decides whether to keep them or
// retry, and applies formatSegments at the end on the chosen result).
// Caller holds t.mu.
func (t *Transcriber) transcribeOnce(pcm []float32, beam int) ([]string, error) {
	t.ctx.SetBeamSize(beam)

	t0 := time.Now()
	if err := t.ctx.Process(pcm, nil, nil, nil); err != nil {
		return nil, fmt.Errorf("process: %w", err)
	}
	processMs := time.Since(t0).Milliseconds()

	t1 := time.Now()
	var segments []string
	for {
		seg, err := t.ctx.NextSegment()
		if err != nil {
			break
		}
		if text := strings.TrimSpace(seg.Text); text != "" {
			segments = append(segments, text)
		}
	}
	segMs := time.Since(t1).Milliseconds()
	log.Printf("transcriber: process=%dms segments=%dms beam=%d", processMs, segMs, beam)
	return segments, nil
}

// looksLikeFastMode heuristically detects whisper's degraded
// high-temperature output: a long-ish run of words with no internal
// punctuation (commas / periods / question / exclamation marks) and
// no uppercase letters anywhere except possibly the first character.
// Short utterances ("привет, как дела") can legitimately have no
// caps/punct, so fastModeMinWords filters those out.
//
// Skipping rules:
//   - The very first letter is ignored. Whisper itself frequently
//     capitalises the leading word even in fast-mode output, and our
//     own pipeline does it later anyway — so a single leading cap is
//     no longer reliable evidence of "Whisper authored proper
//     punctuation".
//   - The very last sentence terminator is ignored. Our
//     finalizeTerminalPunctuation always appends one; that single
//     trailing dot mustn't mask the fast-mode signal.
//
// Internal commas count as positive punctuation signal here:
// real Russian speech of 15+ words almost always has at least one
// comma. A 15-word run with zero commas, zero periods, zero
// uppercase is the high-confidence fast-mode signature.
func looksLikeFastMode(text string) bool {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return false
	}
	if len(strings.Fields(string(runes))) < fastModeMinWords {
		return false
	}
	lastIdx := len(runes) - 1
	for i, r := range runes {
		if i == 0 && unicode.IsLetter(r) {
			continue
		}
		if i == lastIdx && (r == '.' || r == '!' || r == '?') {
			continue
		}
		if unicode.IsUpper(r) {
			return false
		}
		if r == '.' || r == '!' || r == '?' || r == ',' || r == ';' || r == ':' {
			return false
		}
	}
	return true
}

const (
	fastModeMinWords      = 15
	fastModeMaxRetries    = 2
	fastModeSilencePadSec = 0.5
)

// formatSegments runs the post-processing pipeline that turns the
// whisper.cpp Go binding's raw segment list into the text the user
// actually sees. Stages:
//
//  1. normalizeEllipsis:            replace "…" and any run of 2+
//     dots with a single "." — the user never writes ellipsis
//     themselves but the pause it marks is a real sentence
//     boundary; keeping it as a normal period preserves the next
//     word's Whisper-applied capitalisation. Runs before
//     removeHallucinations so the sentence-chunker doesn't split
//     "Подожди..." into bare-dot chunks that would be stripped and
//     silently glue neighbours back together.
//  2. removeHallucinations:        drop known training-data leakage
//     phrases ("Subtitles by …", "Подписывайтесь на канал", etc.)
//     by regex and the CSV-driven exact-match list.
//  3. collapseRepeatedBlocks:      collapse any consecutive block of
//     words that repeats 2+ times (case- and punctuation-insensitive),
//     up to 2 passes — first catches the outer block, second catches
//     blocks revealed inside the first copy. Whisper's stutter loops
//     produce 5–14× copies of long phrases; this is the only filter
//     that reliably cleans them.
//  4. joinSingleWordSentenceRuns:   3+ consecutive single-word
//     "sentences" → join with spaces, keep only the trailing period.
//  5. stripFillerOnlySentences:     standalone sentences that are
//     nothing but discourse-marker filler ("Вот.", "Ну как бы.",
//     "В общем.") → remove.
//  6. stripFillerBetweenCommas:     comma-delimited clauses inside a
//     longer sentence that are pure filler ("Поэтому, типа, давай"
//     → "Поэтому, давай") → drop the clause and its commas. Catches
//     fillers that survive stage 5 because they were already inside
//     a multi-clause sentence.
//  7. dedupeSubstantialSentences:    catch sentence-level Whisper
//     loops that broke up across short fragments and slipped past
//     stage 3's consecutive-block detection. Drops any sentence
//     (5+ words) that appears more than once in the text, keeping
//     the first occurrence. Short sentences like "Да." are left
//     alone — they can legitimately recur.
//  8. addSentenceSpacing:           existing rule — insert a space
//     after .!? where Whisper omitted it ("предложение.второе?Третье!").
//  9. capitalizeAfterSentenceTerminators: capitalise the very first
//     letter of the output and the first letter following any . / !
//     / ? + whitespace. Whisper occasionally forgets caps on
//     legitimate sentence starts, and stripFillerBetweenCommas can
//     leave a lowercase word at sentence start when it removes the
//     leading filler clause ("какие-то. Короче, если..." → "какие-
//     то. если..."). Skips single-letter abbreviations ("т.е.
//     корректно") via a word-length guard, and requires actual
//     whitespace between the terminator and the next letter — so
//     "github.com" / "Node.js" aren't disturbed. Runs after
//     addSentenceSpacing so spaces are already in place.
// 10. finalizeTerminalPunctuation:  strip trailing ,/;/: and ensure
//     the text ends with . / ! / ?, then a trailing space (for paste-
//     time concatenation against following text).
func formatSegments(segments []string) string {
	text := strings.Join(segments, " ")
	// Ellipsis normalisation must go before removeHallucinations:
	// sentenceChunks splits "Подожди... сейчас" on every dot,
	// leaving bare "." chunks between halves; removeHallucinations
	// strips them (empty-normalised key) and glues "Подожди.сейчас"
	// together — addSentenceSpacing then inserts a phantom boundary
	// that wasn't in the audio. Collapsing the dot-cluster to a
	// single period up front avoids that, and preserves the real
	// sentence boundary Whisper saw at that point.
	text = normalizeEllipsis(text)
	text = removeHallucinations(text)
	if text == "" {
		return ""
	}
	text = collapseRepeatedBlocks(text)
	text = joinSingleWordSentenceRuns(text)
	text = stripFillerOnlySentences(text)
	text = stripFillerBetweenCommas(text)
	text = dedupeSubstantialSentences(text)
	text = addSentenceSpacing(text)
	text = capitalizeAfterSentenceTerminators(text)
	text = finalizeTerminalPunctuation(text)
	return text
}

// normalizeEllipsis replaces the single-rune ellipsis "…" and any
// run of two or more ASCII dots with a single "." — preserving the
// sentence boundary Whisper saw at that point in the audio. The
// user never writes ellipsis in their own text and asked us to drop
// the dot-cluster, but keeping it as a normal terminator means the
// following word (which Whisper has already capitalised) reads
// naturally as the start of a new sentence rather than a stranded
// uppercase mid-phrase.
//
// A standalone single dot is left alone — that's already a normal
// sentence terminator.
//
// Adjacent-period collision is handled: "т.д..." (abbreviation
// "т.д." immediately followed by an ellipsis) becomes "т.д." rather
// than "т.д.." — we don't append a period when the builder already
// ends in one.
func normalizeEllipsis(text string) string {
	var sb strings.Builder
	runes := []rune(text)
	writePeriod := func() {
		s := sb.String()
		if len(s) == 0 || s[len(s)-1] != '.' {
			sb.WriteByte('.')
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '…' {
			writePeriod()
			continue
		}
		if r == '.' {
			j := i
			for j < len(runes) && runes[j] == '.' {
				j++
			}
			run := j - i
			if run >= 2 {
				writePeriod()
				i = j - 1
				continue
			}
			sb.WriteRune('.')
			continue
		}
		sb.WriteRune(r)
	}
	return strings.Join(strings.Fields(sb.String()), " ")
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

// collapseRepeatedBlocks finds any consecutive run of N words that
// repeats 2 or more times and replaces the run with a single copy.
// Comparison uses normalizeHallucinationKey, so "Б, А" and "б а"
// count as the same block — Whisper's stutter loops often drift in
// punctuation/case between copies.
//
// Runs at most 2 passes. The first pass catches the outer block; the
// second pass cleans up the inner block exposed inside the kept copy
// of pass 1 (e.g. "А Б А Б В А Б А Б В" → "А Б А Б В" → "А Б В").
// User explicitly capped this — deeper nesting hasn't shown up in
// practice and "да-да" collapsing to "да" is acceptable to them.
//
// At each position we pick the block length that removes the most
// words (blockLen × (copies−1)); ties go to the longer block. This
// handles both flat ("А А А А" → "А") and structured ("hello world
// hello world hello world" → "hello world") repeats in one pass.
func collapseRepeatedBlocks(text string) string {
	words := strings.Fields(text)
	if len(words) < 2 {
		return text
	}
	for pass := 0; pass < 2; pass++ {
		next := collapseRepeatedBlocksOnce(words)
		if len(next) == len(words) {
			break
		}
		words = next
	}
	return strings.Join(words, " ")
}

// maxRepeatBlockLen caps how long a single repeating block can be.
// Each position's worst-case scan is O(maxLen²), so leaving this
// unbounded would make long transcripts (~5k words after a stutter)
// pathologically slow. 100 covers any phrase a real user would
// stutter into a loop.
const maxRepeatBlockLen = 100

func collapseRepeatedBlocksOnce(words []string) []string {
	normalized := make([]string, len(words))
	for i, w := range words {
		normalized[i] = normalizeHallucinationKey(w)
	}
	out := make([]string, 0, len(words))
	i := 0
	for i < len(words) {
		maxLen := (len(words) - i) / 2
		if maxLen > maxRepeatBlockLen {
			maxLen = maxRepeatBlockLen
		}
		bestLen := 0
		bestCopies := 0
		bestRemoved := 0
		for blockLen := maxLen; blockLen >= 1; blockLen-- {
			if !equalNormalized(normalized[i:i+blockLen], normalized[i+blockLen:i+2*blockLen]) {
				continue
			}
			copies := 2
			j := i + 2*blockLen
			for j+blockLen <= len(words) && equalNormalized(normalized[i:i+blockLen], normalized[j:j+blockLen]) {
				copies++
				j += blockLen
			}
			removed := blockLen * (copies - 1)
			if removed > bestRemoved {
				bestLen = blockLen
				bestCopies = copies
				bestRemoved = removed
			}
		}
		if bestLen > 0 {
			out = append(out, words[i:i+bestLen]...)
			i += bestLen * bestCopies
		} else {
			out = append(out, words[i])
			i++
		}
	}
	return out
}

func equalNormalized(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// joinSingleWordSentenceRuns finds runs of 3+ consecutive sentences
// each consisting of a single word and merges them into one phrase,
// keeping only the trailing terminator of the last sentence. Whisper
// chops some fast utterances into per-word "sentences" ("Поэтому. Так.
// Давай."); reading those as a list is wrong, the user meant one
// breath.
//
// Runs of exactly 2 single-word sentences are left alone — they're
// often "Yes. No." style legitimate replies.
func joinSingleWordSentenceRuns(text string) string {
	chunks := sentenceChunks(text)
	if len(chunks) < 3 {
		return text
	}
	out := make([]string, 0, len(chunks))
	i := 0
	for i < len(chunks) {
		if !isSingleWordSentence(chunks[i]) {
			out = append(out, chunks[i])
			i++
			continue
		}
		j := i
		for j < len(chunks) && isSingleWordSentence(chunks[j]) {
			j++
		}
		count := j - i
		if count < 3 {
			out = append(out, chunks[i:j]...)
			i = j
			continue
		}
		// Strip trailing terminator from each chunk except the last;
		// re-attach a single space so the tokens read as one phrase.
		for k := i; k < j-1; k++ {
			out = append(out, stripTrailingSentencePunct(chunks[k])+" ")
		}
		out = append(out, chunks[j-1])
		i = j
	}
	return strings.Join(out, "")
}

func isSingleWordSentence(chunk string) bool {
	trimmed := strings.TrimSpace(chunk)
	if trimmed == "" {
		return false
	}
	runes := []rune(trimmed)
	if !isSentencePunctuation(runes[len(runes)-1]) {
		return false
	}
	if len(strings.Fields(trimmed)) != 1 {
		return false
	}
	// Single-letter "words" are almost always abbreviations like
	// "т." / "д." / "г." — not real sentences. Without this guard,
	// "См. т.д." gets merged as 3 consecutive single-word sentences
	// and loses its periods.
	word := strings.TrimRightFunc(trimmed, isSentencePunctuation)
	return len([]rune(word)) >= 2
}

func stripTrailingSentencePunct(chunk string) string {
	runes := []rune(strings.TrimRightFunc(chunk, unicode.IsSpace))
	end := len(runes)
	for end > 0 && isSentencePunctuation(runes[end-1]) {
		end--
	}
	return string(runes[:end])
}

// fillerPhrases is the normalized-key set of Russian discourse
// markers we treat as filler when they appear as a standalone
// sentence. Multi-token entries ("как бы", "это самое", "в общем")
// are matched as units — isFillerOnlyNormalized tries 2-grams before
// 1-grams. Single occurrences inside a real sentence ("Это вот так
// получилось") are untouched.
// knownAbbreviations are Russian short-form words that end in a
// period without that period being a sentence terminator
// ("См. также", "проф. Иванов", "млн. рублей"). Listed in lower-case
// — the capitalize pass normalises the candidate word before lookup.
//
// Kept deliberately small: only entries common enough that the
// surrounding lowercase word is normal Russian. Single-letter
// abbreviations ("т.", "г.", "д.") are already excluded by the
// general "word before terminator must be 2+ chars" guard, so they
// aren't repeated here.
var knownAbbreviations = map[string]struct{}{
	"см":   {}, // смотри
	"ср":   {}, // сравни
	"стр":  {}, // страница
	"тыс":  {}, // тысяча
	"млн":  {}, // миллион
	"млрд": {}, // миллиард
	"напр": {}, // например
	"проф": {}, // профессор
}

var fillerPhrases = map[string]struct{}{
	"вот":       {},
	"так":       {},
	"значит":    {},
	"короче":    {},
	"ну":        {},
	"типа":      {},
	"как бы":    {},
	"это самое": {},
	"в общем":   {},
}

// stripFillerOnlySentences removes sentence chunks whose normalized
// content tiles entirely with entries from fillerPhrases ("Вот.",
// "Ну как бы.", "В общем."). Runs in the pipeline AFTER
// joinSingleWordSentenceRuns, so only sentences that remained
// standalone get filtered — when 3b merged a long stutter run that
// happened to include filler tokens, the merged sentence (now
// multi-token with surrounding content) is intentionally kept.
func stripFillerOnlySentences(text string) string {
	chunks := sentenceChunks(text)
	if len(chunks) == 0 {
		return text
	}
	out := make([]string, 0, len(chunks))
	changed := false
	for _, chunk := range chunks {
		normalized := normalizeHallucinationKey(chunk)
		if normalized != "" && isFillerOnlyNormalized(normalized) {
			changed = true
			continue
		}
		out = append(out, chunk)
	}
	if !changed {
		return text
	}
	return strings.Join(out, "")
}

func isFillerOnlyNormalized(normalized string) bool {
	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return false
	}
	i := 0
	for i < len(tokens) {
		if i+1 < len(tokens) {
			bigram := tokens[i] + " " + tokens[i+1]
			if _, ok := fillerPhrases[bigram]; ok {
				i += 2
				continue
			}
		}
		if _, ok := fillerPhrases[tokens[i]]; ok {
			i++
			continue
		}
		return false
	}
	return true
}

// stripFillerBetweenCommas walks each sentence's comma-delimited
// clauses and drops any clause whose normalized content is pure
// filler ("Поэтому, типа, в общем, давай" → "Поэтому, давай").
// Catches fillers that survive stripFillerOnlySentences because they
// sat inside a longer sentence rather than standing alone.
//
// Position-insensitive: filler clauses at the start, middle, or end
// of the sentence are all dropped, along with their surrounding
// commas (one comma per dropped clause — the original sentence's
// joining commas are reused).
//
// Sentences that consist entirely of filler clauses (after the drop
// nothing is left) get removed wholesale.
func stripFillerBetweenCommas(text string) string {
	chunks := sentenceChunks(text)
	if len(chunks) == 0 {
		return text
	}
	var sb strings.Builder
	changed := false
	for _, chunk := range chunks {
		cleaned, dropped := dropFillerClauses(chunk)
		if dropped {
			changed = true
		}
		sb.WriteString(cleaned)
	}
	if !changed {
		return text
	}
	return sb.String()
}

// dropFillerClauses processes one sentence chunk: peel off the
// trailing terminator (.!?) + whitespace, split the body by commas,
// drop filler-only clauses, rejoin with commas. Returns the cleaned
// chunk and whether anything actually changed.
//
// If every clause was filler, the result is "" — caller treats that
// as the whole sentence being filler and removes it.
func dropFillerClauses(chunk string) (string, bool) {
	runes := []rune(chunk)
	end := len(runes)
	for end > 0 && (isSentencePunctuation(runes[end-1]) || unicode.IsSpace(runes[end-1])) {
		end--
	}
	body := string(runes[:end])
	trailing := string(runes[end:])
	if !strings.Contains(body, ",") {
		// No commas → nothing to peel apart. Leave as is.
		return chunk, false
	}
	clauses := strings.Split(body, ",")
	kept := make([]string, 0, len(clauses))
	dropped := false
	for _, clause := range clauses {
		normalized := normalizeHallucinationKey(clause)
		if normalized != "" && isFillerOnlyNormalized(normalized) {
			dropped = true
			continue
		}
		kept = append(kept, clause)
	}
	if !dropped {
		return chunk, false
	}
	if len(kept) == 0 {
		// Whole sentence was filler clauses — drop it entirely.
		return "", true
	}
	rejoined := strings.Join(kept, ",")
	// If the first kept clause had a leading space (because it was
	// after a previous comma in the original), trim it — otherwise
	// the sentence starts with " " which addSentenceSpacing leaves
	// alone and finalizeTerminalPunctuation trims later anyway, but
	// cleaner to drop it here.
	rejoined = strings.TrimLeft(rejoined, " ")
	return rejoined + trailing, true
}

// dedupeSubstantialSentences scans the sentence chunks of the text
// and drops any that has already appeared earlier with the same
// normalised form. Only "substantial" sentences (5+ word-tokens
// after normalisation) are deduplicated — short ones like "Да."
// or "Хорошо." can legitimately recur.
//
// collapseRepeatedBlocks (stage 3 of the pipeline) handles the
// usual Whisper stutter where the same sentence repeats N times in
// a row. But sometimes the loop emits a different short fragment
// between copies — that breaks the consecutive-block detection,
// and we end up with [A, frag, A, frag, A, final] where A is the
// same long sentence each time. This pass catches those: any A
// after the first instance is dropped.
const dedupeSubstantialMinWords = 5

func dedupeSubstantialSentences(text string) string {
	chunks := sentenceChunks(text)
	if len(chunks) < 2 {
		return text
	}
	seen := make(map[string]bool)
	var sb strings.Builder
	changed := false
	for _, chunk := range chunks {
		key := normalizeHallucinationKey(chunk)
		if key == "" {
			sb.WriteString(chunk)
			continue
		}
		if len(strings.Fields(key)) >= dedupeSubstantialMinWords {
			if seen[key] {
				changed = true
				continue
			}
			seen[key] = true
		}
		sb.WriteString(chunk)
	}
	if !changed {
		return text
	}
	return sb.String()
}

// capitalizeAfterSentenceTerminators flips the first letter of the
// text — and of any "new sentence" — to uppercase if Whisper left it
// in lowercase.
//
// "New sentence" = position right after a '.' / '!' / '?' followed
// by whitespace, provided the word ending at that terminator is at
// least 2 letters/digits. The length guard mirrors the one in
// stripStrayMidSentencePeriods, so abbreviations like "т.е.
// корректно" — where the period is part of the abbreviation rather
// than a sentence end — are left alone.
//
// This runs after stripFillerBetweenCommas because that filter can
// drop a leading filler clause ("Короче,") from a sentence,
// promoting what used to be a mid-sentence lowercase word into
// sentence-initial position; capitalising here cleans that up.
func capitalizeAfterSentenceTerminators(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}

	// 1. Capitalise the very first letter of the text — unless the
	//    leading "word" contains an internal period, in which case
	//    it's almost certainly an identifier the user spoke literally
	//    ("github.com", "node.js", "README.md") and we mustn't touch
	//    its casing.
	firstLetterAt := -1
	firstWordEnd := len(runes)
	for i, r := range runes {
		if unicode.IsLetter(r) {
			firstLetterAt = i
			break
		}
		if !unicode.IsSpace(r) {
			// Leading non-space non-letter (quote, bracket, …) — give
			// up on the very-first-letter rule.
			break
		}
	}
	if firstLetterAt >= 0 {
		for i := firstLetterAt; i < len(runes); i++ {
			if unicode.IsSpace(runes[i]) {
				firstWordEnd = i
				break
			}
		}
		hasDot := false
		for i := firstLetterAt; i < firstWordEnd; i++ {
			if runes[i] == '.' {
				hasDot = true
				break
			}
		}
		if !hasDot && unicode.IsLower(runes[firstLetterAt]) {
			runes[firstLetterAt] = unicode.ToUpper(runes[firstLetterAt])
		}
	}

	// 2. Capitalise the first letter after every .!? + whitespace,
	//    when the word ending at the terminator is 2+ chars and
	//    isn't a known abbreviation.
	for i := 0; i < len(runes); i++ {
		if !isSentencePunctuation(runes[i]) {
			continue
		}
		// Must have at least one whitespace rune after the terminator
		// — otherwise we're inside an identifier like "github.com" or
		// "Node.js" and the period is structural, not a sentence end.
		if i+1 >= len(runes) || !unicode.IsSpace(runes[i+1]) {
			continue
		}
		// Word before the terminator: walk back over letters,
		// digits AND hyphens — "какие-то" is one logical word and
		// we want its full length counted, not just "то".
		wordStart := i - 1
		for wordStart >= 0 && (unicode.IsLetter(runes[wordStart]) || unicode.IsDigit(runes[wordStart]) || runes[wordStart] == '-') {
			wordStart--
		}
		if i-(wordStart+1) < 2 {
			// Single-letter abbreviation-like ("т." / "е." / "5.")
			// — don't treat the following lowercase word as a new
			// sentence.
			continue
		}
		// Strip the hyphen from the leading edge if walk-back
		// landed on one (shouldn't happen since we stop at non-
		// letter/digit/hyphen, but be defensive).
		word := strings.ToLower(string(runes[wordStart+1 : i]))
		if _, abbr := knownAbbreviations[word]; abbr {
			continue
		}
		// Next non-whitespace rune.
		j := i + 2
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			j++
		}
		if j >= len(runes) {
			break
		}
		if unicode.IsLower(runes[j]) {
			runes[j] = unicode.ToUpper(runes[j])
		}
	}

	return string(runes)
}

// finalizeTerminalPunctuation ensures the text ends with a sentence
// terminator and a trailing space (the downstream paster expects the
// space). It also strips any trailing ,/;/: that block-collapse may
// have left over: when the first copy of a repeated block originally
// led into the next copy with a comma, the kept first copy retains
// that comma after the collapse — readers don't want "А Б, ." in the
// output.
func finalizeTerminalPunctuation(text string) string {
	runes := []rune(strings.TrimSpace(text))
	end := len(runes)
	for end > 0 {
		r := runes[end-1]
		if r == ',' || r == ';' || r == ':' || unicode.IsSpace(r) {
			end--
			continue
		}
		break
	}
	if end == 0 {
		return ""
	}
	s := string(runes[:end])
	last := []rune(s)[end-1]
	if !isSentencePunctuation(last) && last != '…' {
		s += "."
	}
	return s + " "
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
