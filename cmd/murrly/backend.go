package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/gpucheck"
	"github.com/tertiumorganum1/murrly/internal/paths"
)

// cpuModelName is the model CPU inference is steered to. large-v3-turbo on
// the CPU is minutes per dictation; the q5_0 quantization of the same model
// is a few times smaller and is the only variant worth waiting for without a
// card. Steered, not forced — if the file was never downloaded we stay on
// whatever the config names rather than failing to start.
const cpuModelName = "large-v3-turbo-q5_0"

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
	quantized := filepath.Join(dir, "ggml-"+cpuModelName+".bin")
	if quantized == configured {
		return configured
	}
	if _, err := os.Stat(quantized); err != nil {
		log.Printf("cpu: %s не скачана, остаёмся на %s — на процессоре это будет долго",
			filepath.Base(quantized), filepath.Base(configured))
		return configured
	}
	log.Printf("cpu: модель %s вместо %s", filepath.Base(quantized), filepath.Base(configured))
	return quantized
}

// backendLabel is the word that goes in the log and in the tray tooltip.
func backendLabel(useGPU bool) string {
	if useGPU {
		return "видеокарта"
	}
	return "процессор"
}
