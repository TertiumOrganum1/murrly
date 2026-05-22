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
	ModelPath     string `toml:"model_path"`
	Device        string `toml:"device"`
	ComputeType   string `toml:"compute_type"`
	Language      string `toml:"language"`
	BeamSize      int    `toml:"beam_size"`
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
			ModelPath:     "", // resolved in Load() from paths.ModelsDir() if empty
			Device:        "cuda",
			ComputeType:   "float16",
			Language:      "",
			BeamSize:      5,
			InitialPrompt: "Мы обсуждаем программирование и архитектуру: React, TypeScript, Docker, Kubernetes, microservices, middleware, observability.",
		},
		Output: OutputConfig{PasteDelayMs: 80, RestorePrimary: true},
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

	// Resolve ModelPath from paths.ModelsDir() if not set in the config.
	if cfg.Whisper.ModelPath == "" {
		dir, err := paths.ModelsDir()
		if err != nil {
			return Config{}, fmt.Errorf("models dir: %w", err)
		}
		cfg.Whisper.ModelPath = filepath.Join(dir, "ggml-large-v3.bin")
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
