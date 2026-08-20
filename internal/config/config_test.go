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
	if cfg.Output.PasteDelayMs != 250 {
		t.Errorf("default paste delay: got %d, want 250", cfg.Output.PasteDelayMs)
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

func TestInsertModeDefaultsAndNormalisation(t *testing.T) {
	dir := t.TempDir()

	// Fresh install: hybrid is the default, with a usable type delay.
	cfg, err := Load(filepath.Join(dir, "fresh.toml"))
	if err != nil {
		t.Fatalf("Load fresh: %v", err)
	}
	if cfg.Output.InsertMode != InsertClipboard {
		t.Errorf("default insert_mode: got %q, want %q", cfg.Output.InsertMode, InsertClipboard)
	}
	if cfg.Output.TypeDelayMs <= 0 {
		t.Errorf("default type_delay_ms: got %d, want > 0", cfg.Output.TypeDelayMs)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		// An old config predates the setting entirely — it must not end up
		// with an empty mode that inserts nothing.
		{"missing", "[output]\npaste_delay_ms = 250\n", InsertClipboard},
		{"explicit", "[output]\ninsert_mode = \"type\"\n", InsertType},
		{"mixed case and spaces", "[output]\ninsert_mode = \" Hybrid \"\n", InsertHybrid},
		{"unknown value falls back", "[output]\ninsert_mode = \"telepathy\"\n", InsertClipboard},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".toml")
			if err := os.WriteFile(path, []byte(c.body), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Output.InsertMode != c.want {
				t.Errorf("insert_mode: got %q, want %q", cfg.Output.InsertMode, c.want)
			}
		})
	}
}
