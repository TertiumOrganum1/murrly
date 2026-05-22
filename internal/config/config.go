// Package config loads and validates the murrly TOML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/tertiumorganum1/murrly/internal/paths"
)

type Config struct {
	Hotkey  HotkeyConfig  `toml:"hotkey"`
	Audio   AudioConfig   `toml:"audio"`
	Whisper WhisperConfig `toml:"whisper"`
	Output  OutputConfig  `toml:"output"`
}

type HotkeyConfig struct {
	Key  string `toml:"key"`
	Mode string `toml:"mode"`
}

type AudioConfig struct {
	Device     string `toml:"device"`
	SampleRate int    `toml:"sample_rate"`
}

type WhisperConfig struct {
	// Model is a short name like "large-v3", "large-v3-turbo",
	// "large-v3-turbo-q5_0". Resolved to <ModelsDir>/ggml-<Model>.bin.
	// Takes precedence over ModelPath when set.
	Model         string `toml:"model"`
	// ModelPath is an absolute or ~-expanded path to a .bin file.
	// Used directly if set and Model is empty.
	ModelPath     string `toml:"model_path"`
	Device        string `toml:"device"`
	ComputeType   string `toml:"compute_type"`
	Language      string `toml:"language"`
	BeamSize      int    `toml:"beam_size"`
	// Adaptive — opt-in: short clips use beam_size=1 (greedy), clips
	// past the long-audio threshold bump to 2 (beam_search). Useful on
	// macOS where the default beam stays at 1 for speed, but long
	// dictations would otherwise lose punctuation.
	Adaptive      bool   `toml:"adaptive"`
	InitialPrompt string `toml:"initial_prompt"`
}

type OutputConfig struct {
	PasteDelayMs   int  `toml:"paste_delay_ms"`
	RestorePrimary bool `toml:"restore_primary"`
}

func defaults() Config {
	return Config{
		Hotkey: HotkeyConfig{Key: "F12", Mode: "push_to_talk"},
		Audio:  AudioConfig{Device: "", SampleRate: 16000},
		Whisper: WhisperConfig{
			// Model = "" means "use the legacy default ggml-large-v3.bin".
			// That matches what scripts/bootstrap-{ubuntu,mac}.sh download
			// by default (MODEL=large-v3). Users who run MODELS=all can
			// switch via the tray menu, which writes a non-empty Model
			// short-name back to config.
			Model:         "",
			ModelPath:     "", // optional absolute path; ignored if Model is set
			Device:        "cuda",
			ComputeType:   "float16",
			Language:      "",
			BeamSize:      defaultBeamSize(), // platform-tuned: Linux 2, macOS 1
			Adaptive:      false,             // opt-in; set true to get short=1 / long=2 dynamic switching
			InitialPrompt: "Мы обсуждаем программирование и архитектуру: React, TypeScript, Docker, Kubernetes, microservices, middleware, observability.",
		},
		// PasteDelayMs sits between Set-clipboard / Cmd-V and the Restore-clipboard
		// step. Too short and the focused app reads the restored (old) clipboard
		// mid-paste, garbling output. 250ms is safe on M1 macOS; Linux/xclip
		// tolerates lower values.
		Output: OutputConfig{PasteDelayMs: 250, RestorePrimary: true},
	}
}

// Load reads config from path. If the file doesn't exist, it writes defaults
// and returns them. Missing fields in an existing file are filled from defaults.
func Load(path string) (Config, error) {
	cfg := defaults()

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeDefault(path); err != nil {
			return Config{}, fmt.Errorf("write default config: %w", err)
		}
	} else {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode %s: %w", path, err)
		}
	}

	// Resolve the model file path. Precedence:
	//   1. Whisper.Model (short name)  → <ModelsDir>/ggml-<Model>.bin
	//   2. Whisper.ModelPath           → used as-is (after ~-expansion)
	//   3. neither set                 → <ModelsDir>/ggml-large-v3.bin (legacy default)
	if cfg.Whisper.Model != "" {
		dir, err := paths.ModelsDir()
		if err != nil {
			return Config{}, fmt.Errorf("models dir: %w", err)
		}
		cfg.Whisper.ModelPath = filepath.Join(dir, "ggml-"+cfg.Whisper.Model+".bin")
	} else if cfg.Whisper.ModelPath == "" {
		dir, err := paths.ModelsDir()
		if err != nil {
			return Config{}, fmt.Errorf("models dir: %w", err)
		}
		// Legacy default. Set Model too so the tray/dock model-picker
		// shows the correct checkmark on a fresh install instead of "no
		// active model" (-1).
		cfg.Whisper.Model = "large-v3"
		cfg.Whisper.ModelPath = filepath.Join(dir, "ggml-"+cfg.Whisper.Model+".bin")
	}

	expandPaths(&cfg)
	return cfg, nil
}

func expandPaths(cfg *Config) {
	cfg.Whisper.ModelPath = expandPath(cfg.Whisper.ModelPath)
}

func expandPath(path string) string {
	path = os.ExpandEnv(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func writeDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(defaults())
}

// DefaultPath returns the platform-correct path to murrly's config.toml.
func DefaultPath() (string, error) {
	return paths.ConfigFile()
}
