package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/tertiumorganum1/murrly/internal/config"
	"github.com/tertiumorganum1/murrly/internal/paths"
	"github.com/tertiumorganum1/murrly/internal/transcriber"
)

// transcriberLoader wraps *transcriber.Transcriber so the active Whisper
// model can be swapped while the app keeps running. F12 transcriptions
// share an RWMutex with reloads — a reload (~1s for q5_0, ~1.5s for
// large-v3 on M1 Pro Metal) briefly blocks new Transcribe calls instead
// of the alternative of process-restarting Murrly entirely.
type transcriberLoader struct {
	mu  sync.RWMutex
	tr  *transcriber.Transcriber
	cfg config.WhisperConfig
}

func newTranscriberLoader(initial *transcriber.Transcriber, cfg config.WhisperConfig) *transcriberLoader {
	return &transcriberLoader{tr: initial, cfg: cfg}
}

// Transcribe implements app.Transcriber so the loader can be plugged in
// where the bare Transcriber would have gone.
func (l *transcriberLoader) Transcribe(pcm []float32) (string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.tr.Transcribe(pcm)
}

// Reload swaps the underlying Transcriber to one loaded with modelName.
// The previous Transcriber is closed (frees ~3 GB of Metal/CUDA buffers).
// Safe to call concurrently with Transcribe — readers see either the old
// or new transcriber atomically.
func (l *transcriberLoader) Reload(modelName string) error {
	dir, err := paths.ModelsDir()
	if err != nil {
		return fmt.Errorf("models dir: %w", err)
	}
	modelPath := filepath.Join(dir, "ggml-"+modelName+".bin")

	newCfg := l.cfg
	newCfg.Model = modelName
	newCfg.ModelPath = ""

	newTr, err := transcriber.New(transcriber.Config{
		ModelPath:     modelPath,
		Language:      newCfg.Language,
		BeamSize:      newCfg.BeamSize,
		BeamAdaptive:  newCfg.BeamAdaptive,
		InitialPrompt: newCfg.InitialPrompt,
	})
	if err != nil {
		return fmt.Errorf("load model %q: %w", modelName, err)
	}

	l.mu.Lock()
	old := l.tr
	l.tr = newTr
	l.cfg = newCfg
	l.mu.Unlock()

	_ = old.Close()
	log.Printf("transcriber: hot-swapped → %s", modelName)
	return nil
}

// ReloadConfig re-reads config.toml from disk and rebuilds the
// Transcriber with the new Whisper settings (model, beam_size, language,
// initial_prompt). Lets the user tune Whisper parameters live, without
// restarting the app. Hotkey, audio device, and paste delay are wired
// into other subsystems at startup — those still need a full restart.
func (l *transcriberLoader) ReloadConfig(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	newTr, err := transcriber.New(transcriber.Config{
		ModelPath:     cfg.Whisper.ModelPath,
		Language:      cfg.Whisper.Language,
		BeamSize:      cfg.Whisper.BeamSize,
		BeamAdaptive:  cfg.Whisper.BeamAdaptive,
		InitialPrompt: cfg.Whisper.InitialPrompt,
	})
	if err != nil {
		return fmt.Errorf("rebuild transcriber: %w", err)
	}

	l.mu.Lock()
	old := l.tr
	l.tr = newTr
	l.cfg = cfg.Whisper
	l.mu.Unlock()

	_ = old.Close()
	log.Printf("transcriber: config reloaded (model=%s beam=%d lang=%q)",
		cfg.Whisper.Model, cfg.Whisper.BeamSize, cfg.Whisper.Language)
	return nil
}

// persistModelChoice writes the new model name into config.toml so the
// choice survives a restart. Reuses the whole config struct so we don't
// drop other user-edited values.
func persistModelChoice(cfgPath string, cfg config.Config, modelName string) error {
	cfg.Whisper.Model = modelName
	cfg.Whisper.ModelPath = ""
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistPadSilence writes the new pad-silence state to config.toml.
// Same pattern as persistModelChoice — re-encode the whole struct so
// no other live edits are clobbered.
func persistPadSilence(cfgPath string, cfg config.Config, on bool) error {
	cfg.Whisper.PadSilence = on
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistMultiInference writes the multi-inference on/off toggle to
// config.toml so the menu choice survives a restart. Same whole-struct
// re-encode as the other persist helpers.
func persistMultiInference(cfgPath string, cfg config.Config, on bool) error {
	cfg.Whisper.MultiInference = on
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistScoringMode writes the multi-inference scoring mode to
// config.toml so the menu choice survives a restart. Same whole-struct
// re-encode as the other persist helpers.
func persistScoringMode(cfgPath string, cfg config.Config, mode string) error {
	cfg.Whisper.ScoringMode = mode
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistProfanityFilter writes the profanity-filter on/off toggle to
// config.toml so the menu choice survives a restart. Same whole-struct
// re-encode as the other persist helpers.
func persistProfanityFilter(cfgPath string, cfg config.Config, on bool) error {
	cfg.Output.ProfanityFilter = on
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistProfanityRemove writes the cut-out-vs-mask choice to config.toml.
func persistProfanityRemove(cfgPath string, cfg config.Config, on bool) error {
	cfg.Output.ProfanityRemove = on
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistPreferWireless writes the prefer-wireless-mic toggle to config.toml
// so the menu choice survives a restart. Same whole-struct re-encode as the
// other persist helpers.
func persistPreferWireless(cfgPath string, cfg config.Config, on bool) error {
	cfg.Audio.PreferWireless = on
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// persistNemotronEnabled writes the Nemotron on/off toggle to config.toml
// so the menu choice survives a restart. Same whole-struct re-encode as the
// other persist helpers.
func persistNemotronEnabled(cfgPath string, cfg config.Config, on bool) error {
	cfg.Nemotron.Enabled = on
	f, err := os.Create(cfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
