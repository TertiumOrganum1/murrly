package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Hotkey.Key != "F12" {
		t.Errorf("default hotkey: got %q, want F12", cfg.Hotkey.Key)
	}
	if cfg.Output.PasteDelayMs != 80 {
		t.Errorf("default paste delay: got %d, want 80", cfg.Output.PasteDelayMs)
	}
	if strings.Contains(cfg.Whisper.ModelPath, "~") {
		t.Errorf("default model path was not expanded: %q", cfg.Whisper.ModelPath)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected default config file to be created at %s: %v", path, err)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[hotkey]
key = "F8"

[output]
paste_delay_ms = 200
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Hotkey.Key != "F8" {
		t.Errorf("hotkey: got %q, want F8", cfg.Hotkey.Key)
	}
	if cfg.Output.PasteDelayMs != 200 {
		t.Errorf("paste delay: got %d, want 200", cfg.Output.PasteDelayMs)
	}
	if !cfg.Output.RestorePrimary {
		t.Errorf("RestorePrimary should keep default true when not in file")
	}
}

func TestLoadExpandsModelPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[whisper]
model_path = "~/voice-input-test-model.bin"
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if strings.Contains(cfg.Whisper.ModelPath, "~") {
		t.Fatalf("model path was not expanded: %q", cfg.Whisper.ModelPath)
	}
}
