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
	Hotkey   HotkeyConfig   `toml:"hotkey"`
	Audio    AudioConfig    `toml:"audio"`
	Whisper  WhisperConfig  `toml:"whisper"`
	Nemotron NemotronConfig `toml:"nemotron"`
	Output   OutputConfig   `toml:"output"`
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
	Model string `toml:"model"`
	// ModelPath is an absolute or ~-expanded path to a .bin file.
	// Used directly if set and Model is empty.
	ModelPath   string `toml:"model_path"`
	Device      string `toml:"device"`
	ComputeType string `toml:"compute_type"`
	Language    string `toml:"language"`
	BeamSize    int    `toml:"beam_size"`
	// BeamAdaptive — opt-in: short clips use beam_size=1 (effectively
	// greedy), clips past the long-audio threshold use the upstream
	// beam_search default of 5. Useful on macOS where the configured
	// beam stays at 1 for speed, but long dictations would otherwise
	// risk losing punctuation. Visible in the default config so users
	// know the knob exists.
	BeamAdaptive bool `toml:"beam_adaptive"`
	// PadSilence — when true, every clip is wrapped in 1 s of zero
	// samples at both ends before reaching Whisper. Each manual
	// "Перепроцессить" click then stacks another 1 s of leading
	// silence on top. Exposed in config.toml so the default state
	// survives restarts; flipped at runtime via the tray's "Тишина
	// по краям" toggle.
	PadSilence bool `toml:"pad_silence"`
	// MultiInferenceCount — how many parallel inference variants to run
	// per recording. 1 = single pass (current behavior, no picker).
	// 2..8 = that many Whisper contexts run the same audio with
	// different leading-silence shifts; the best-scoring result is
	// inserted and the rest are cached for the Alt+F12 picker. Clamped
	// to [1,8] on load. Platform-tuned default (Linux 4, macOS 1).
	MultiInferenceCount int `toml:"multi_inference_count"`
	// ScoringMode picks how multi-inference ranks its variants and which
	// one is auto-inserted: "combined" (Whisper confidence + text-shape
	// heuristic, the default), "confidence" (Whisper probability only),
	// or "heuristic" (text-shape only). Switchable live from the tray.
	// Empty is normalized to "combined" on load. Ignored when
	// multi_inference_count == 1.
	ScoringMode string `toml:"scoring_mode"`
	// MultiInference is the live on/off switch for multi-inference. When
	// multi_inference_count > 1 the engine is always built, but this flag
	// decides whether F12 runs the full variant batch (true) or just a
	// single pass over the original sample (false) — the latter avoids the
	// extra latency and the Ctrl+F11 picker entirely. Default true; flipped
	// at runtime via the "Множественное распознавание" menu toggle and
	// persisted here. No effect when multi_inference_count == 1.
	MultiInference bool   `toml:"multi_inference"`
	InitialPrompt  string `toml:"initial_prompt"`
}

type OutputConfig struct {
	PasteDelayMs   int  `toml:"paste_delay_ms"`
	RestorePrimary bool `toml:"restore_primary"`
}

// NemotronConfig configures the second engine (Linux-only; the Break key).
// The model runs in an external Python sidecar served over a Unix socket.
type NemotronConfig struct {
	// Enabled wires the Break key to Nemotron and the F12 background fill.
	// Default true on a fresh config; set false to disable the engine
	// entirely (Break becomes a no-op). No effect on non-Linux builds.
	Enabled bool `toml:"enabled"`
	// SocketPath is the sidecar's Unix socket. Empty → /run/user/<uid>/murrly-nemotron.sock.
	SocketPath string `toml:"socket_path"`
	// Lang is the target language prompt (e.g. "ru-RU").
	Lang string `toml:"lang"`
	// BoostAlpha is the context-biasing weight (0 = off). >0.5 starts to
	// mangle ordinary speech, so 0.5 is the tuned default.
	BoostAlpha float64 `toml:"boost_alpha"`
}

func defaults() Config {
	return Config{
		Hotkey: HotkeyConfig{Key: "F12", Mode: "push_to_talk"},
		Audio:  AudioConfig{Device: "", SampleRate: 16000},
		Whisper: WhisperConfig{
			// Model = "" means "use the default ggml-large-v3-turbo.bin".
			// That matches what scripts/bootstrap-{ubuntu,mac}.sh download
			// by default (MODEL=large-v3-turbo). Users who run MODELS=all can
			// switch via the tray menu, which writes a non-empty Model
			// short-name back to config.
			Model:               "",
			ModelPath:           "", // optional absolute path; ignored if Model is set
			Device:              "cuda",
			ComputeType:         "float16",
			Language:            "",
			BeamSize:            defaultBeamSize(),            // platform-tuned: Linux 5, macOS 1
			BeamAdaptive:        false,                        // opt-in; set true to get short=1 / long=5 dynamic switching
			PadSilence:          false,                        // opt-in; wrap every clip in 1 s silence padding
			MultiInferenceCount: defaultMultiInferenceCount(), // platform-tuned: Linux 4, macOS 2
			ScoringMode:         "combined",                   // confidence + heuristic blend; switchable from the tray
			MultiInference:      true,                         // live on/off for the variant batch; toggled from the menu
			InitialPrompt:       "Мы обсуждаем программирование и архитектуру: React, TypeScript, Docker, Kubernetes, microservices, middleware, observability.",
		},
		// Nemotron: second engine on the Break key. Enabled by default; the
		// sidecar must be running (systemd user service) for it to respond.
		Nemotron: NemotronConfig{Enabled: true, SocketPath: "", Lang: "ru-RU", BoostAlpha: 0.5},
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
	//   3. neither set                 → <ModelsDir>/ggml-large-v3-turbo.bin (default)
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
		// Default model. Set Model too so the tray/dock model-picker
		// shows the correct checkmark on a fresh install instead of "no
		// active model" (-1).
		cfg.Whisper.Model = "large-v3-turbo"
		cfg.Whisper.ModelPath = filepath.Join(dir, "ggml-"+cfg.Whisper.Model+".bin")
	}

	// Clamp multi-inference count into the supported range. 0 (unset in
	// an old config) becomes the platform default; anything past 8 is
	// capped to keep VRAM bounded.
	if cfg.Whisper.MultiInferenceCount == 0 {
		cfg.Whisper.MultiInferenceCount = defaultMultiInferenceCount()
	}
	if cfg.Whisper.MultiInferenceCount < 1 {
		cfg.Whisper.MultiInferenceCount = 1
	}
	if cfg.Whisper.MultiInferenceCount > 8 {
		cfg.Whisper.MultiInferenceCount = 8
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
