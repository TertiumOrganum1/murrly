package parakeet

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The model is worth loading for real rather than mocking: everything that can
// go wrong here goes wrong inside sherpa-onnx, silently — a feature dimension
// that does not match the encoder, a model type it sniffs differently, a
// tokens file it reads but cannot map. None of those fail the load; they come
// back as text that is confidently wrong. So: decode the sample the release
// ships and check the words.
//
// Skipped in -short and when the model is not downloaded.
func TestRecognizesSampleWav(t *testing.T) {
	if testing.Short() {
		t.Skip("loads a 600 MB model from disk")
	}
	if !Present() {
		t.Skip("parakeet-v3 not downloaded")
	}
	dir, err := Dir()
	if err != nil {
		t.Skipf("models dir: %v", err)
	}
	wav := filepath.Join(dir, "test_wavs", "en.wav")
	pcm, err := readWav16(wav)
	if err != nil {
		t.Skipf("%s: %v", filepath.Base(wav), err)
	}

	r, err := New(0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer r.Close()

	text, err := r.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("empty transcription of the sample wav")
	}
	// The sample is English speech; any correct decode has ordinary words in
	// it. A wrong feature dim or model type produces punctuation soup or a
	// single repeated token, which this catches without pinning the exact
	// sentence (the model may be updated upstream).
	if words := strings.Fields(text); len(words) < 3 {
		t.Fatalf("suspiciously short transcription: %q", text)
	}
	t.Logf("sample decoded as: %q", text)
}

// readWav16 reads a mono 16-bit PCM WAV into the float32 samples the
// recogniser expects. Deliberately minimal — it reads the files this model
// ships with, not arbitrary WAVs.
func readWav16(path string) ([]float32, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Walk the RIFF chunks to "data" rather than assuming a 44-byte header:
	// these files carry a LIST chunk before the samples.
	pos := 12
	for pos+8 <= len(raw) {
		id := string(raw[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
		body := pos + 8
		if id == "data" {
			end := body + size
			if end > len(raw) {
				end = len(raw)
			}
			samples := make([]float32, (end-body)/2)
			for i := range samples {
				v := int16(binary.LittleEndian.Uint16(raw[body+2*i:]))
				samples[i] = float32(v) / math.MaxInt16
			}
			return samples, nil
		}
		pos = body + size + size%2
	}
	return nil, os.ErrInvalid
}
