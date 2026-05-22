package main

import (
	"log"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/tertiumorganum1/murrly/internal/config"
)

// switchModelAndRestart writes the new model name to the user config,
// spawns a fresh Murrly process via LaunchServices (macOS) or self-exec
// (Linux), and exits the current process. The new instance starts with
// the new model loaded.
//
// We use a hard restart instead of an in-process model swap because:
//   - whisper.cpp keeps ~3 GB of Metal/CUDA buffers attached to the
//     loaded model; freeing and reallocating those without leaking is
//     tricky, and the cost of a clean restart (~1.5 s of model load) is
//     comparable.
//   - On macOS the restart goes through LaunchServices so the Dock icon
//     and the Accessibility/Microphone TCC permissions stay intact.
func switchModelAndRestart(cfgPath string, cfg config.Config, newModel string) {
	cfg.Whisper.Model = newModel
	cfg.Whisper.ModelPath = "" // force re-resolve from Model

	if err := writeConfig(cfgPath, cfg); err != nil {
		log.Printf("switch model: write config: %v", err)
		return
	}
	log.Printf("switch model: → %s, restarting", newModel)

	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", "-a", "Murrly").Start()
	} else {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("switch model: cannot resolve executable: %v", err)
			return
		}
		cmd := exec.Command(exe)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.Stdin = nil
		if err := cmd.Start(); err != nil {
			log.Printf("switch model: cannot spawn self: %v", err)
			return
		}
	}

	// Give LaunchServices / setsid a moment to take ownership before we
	// terminate, otherwise the child can be killed in cgroup teardown.
	time.Sleep(150 * time.Millisecond)
	os.Exit(0)
}

func writeConfig(path string, cfg config.Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
