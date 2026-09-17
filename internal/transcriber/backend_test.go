package transcriber

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tertiumorganum1/murrly/internal/paths"
)

// The CPU path is worth a real load rather than a mock: use_gpu is a
// model-load parameter that only the binding knows about, so the thing that
// can break — silently, in a CUDA build where everything else still works —
// is exactly whether whisper.cpp honours it and produces a usable context.
//
// Skipped in -short and when the quantized model is not downloaded, so it
// never blocks a machine that has no models at hand.
func TestModelLoadsOnCPU(t *testing.T) {
	if testing.Short() {
		t.Skip("loads a model from disk")
	}
	dir, err := paths.ModelsDir()
	if err != nil {
		t.Skipf("models dir: %v", err)
	}
	path := filepath.Join(dir, "ggml-large-v3-turbo-q5_0.bin")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not downloaded", filepath.Base(path))
	}

	tr, err := New(Config{ModelPath: path, Language: "ru", BeamSize: 1, UseGPU: false})
	if err != nil {
		t.Fatalf("load on CPU: %v", err)
	}
	defer tr.Close()

	// Half a second of silence. The text is whatever the model makes of
	// nothing (usually empty); what is under test is that inference runs to
	// completion on a context built without the GPU.
	if _, err := tr.Transcribe(make([]float32, pcmSampleRateHz/2)); err != nil {
		t.Fatalf("transcribe on CPU: %v", err)
	}
}
