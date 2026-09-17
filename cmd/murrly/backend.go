package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/gpucheck"
	"github.com/tertiumorganum1/murrly/internal/paths"
)

// cpuModelNames are the models CPU inference is steered to, best first.
// large-v3-turbo on the CPU is minutes per dictation; the q5_0 quantization of
// the same model is a few times smaller and is the only variant worth waiting
// for without a card.
//
// medium-q5_0 goes first: on this processor it encodes in roughly half the
// time of the large family and still holds Russian. small-q5_1 is faster
// again and is deliberately NOT here — on Russian dictation it produced a
// Spanish hallucination on one phrase and an empty string on the next, and a
// fast wrong answer is not a faster Murrly. It stays reachable through the
// tray model picker for anyone who wants to judge it themselves.
//
// Steered, not forced — if none of these was downloaded we stay on whatever
// the config names rather than failing to start.
var cpuModelNames = []string{"medium-q5_0", "large-v3-turbo-q5_0"}

// chooseBackend decides where the model is loaded and which file is loaded,
// from the configured device plus what the cards actually have free.
//
// The VRAM check is not an optimisation. ggml allocates the weights buffer by
// buffer, and a cudaMalloc that fails part-way through leaves the driver
// holding memory that nothing gives back until a reboot — nvidia-smi shows it
// charged to a PID that no longer exists. So the arithmetically hopeless case
// (less free VRAM than the weights alone occupy) never reaches the loader: it
// starts on the CPU instead, which is slow but is a working Murrly.
func chooseBackend(w config.WhisperConfig) (useGPU bool, modelPath string) {
	if w.Device == config.DeviceCPU {
		return false, cpuModelPath(w.ModelPath)
	}
	if err := gpucheck.EnsureFree(w.ModelPath); err != nil {
		log.Printf("gpucheck: %v — распознавание пойдёт на процессоре", err)
		desktopNotify("Murrly", "Не хватает видеопамяти — распознавание идёт на процессоре, это заметно медленнее.")
		return false, cpuModelPath(w.ModelPath)
	}
	return true, w.ModelPath
}

// cpuModelPath swaps a configured model for the quantized one when running on
// the CPU, provided that file is actually present.
func cpuModelPath(configured string) string {
	dir, err := paths.ModelsDir()
	if err != nil {
		return configured
	}
	for _, name := range cpuModelNames {
		candidate := filepath.Join(dir, "ggml-"+name+".bin")
		if candidate == configured {
			return configured
		}
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		log.Printf("cpu: модель %s вместо %s", filepath.Base(candidate), filepath.Base(configured))
		return candidate
	}
	log.Printf("cpu: быстрая модель не скачана, остаёмся на %s — на процессоре это будет долго",
		filepath.Base(configured))
	return configured
}

// whisperFallbackPath is the model file the Whisper side is pointed at when the
// config names Parakeet. Parakeet has no ggml file, but the Whisper engine is
// still built underneath it — that is what makes a switch back a menu click
// instead of a model load — and it has to be built from something that exists.
//
// Returns "" when no Whisper model is downloaded at all, which leaves the
// caller on the configured path and its existing failure handling.
func whisperFallbackPath() string {
	dir, err := paths.ModelsDir()
	if err != nil {
		return ""
	}
	for _, name := range append(append([]string{}, cpuModelNames...), "large-v3-turbo") {
		candidate := filepath.Join(dir, "ggml-"+name+".bin")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// backendLabel is the word that goes in the log and in the tray tooltip.
func backendLabel(useGPU bool) string {
	if useGPU {
		return "видеокарта"
	}
	return "процессор"
}
