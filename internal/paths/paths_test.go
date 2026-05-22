package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigDir(t *testing.T) {
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if !strings.HasSuffix(got, "murrly") && !strings.HasSuffix(got, "Murrly") {
		t.Errorf("ConfigDir = %q, want suffix murrly or Murrly", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("ConfigDir = %q, want absolute path", got)
	}
}

func TestConfigFile(t *testing.T) {
	got, err := ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if filepath.Base(got) != "config.toml" {
		t.Errorf("ConfigFile basename = %q, want config.toml", filepath.Base(got))
	}
}

func TestModelsDir(t *testing.T) {
	got, err := ModelsDir()
	if err != nil {
		t.Fatalf("ModelsDir: %v", err)
	}
	if filepath.Base(got) != "models" {
		t.Errorf("ModelsDir basename = %q, want models", filepath.Base(got))
	}
}

func TestCacheDir(t *testing.T) {
	got, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("CacheDir = %q, want absolute path", got)
	}
}

func TestLinuxConventions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux only")
	}
	home, _ := os.UserHomeDir()
	cfg, _ := ConfigDir()
	if got, want := cfg, filepath.Join(home, ".config", "murrly"); got != want {
		t.Errorf("ConfigDir = %q, want %q", got, want)
	}
	data, _ := DataDir()
	if got, want := data, filepath.Join(home, ".local", "share", "murrly"); got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
	cache, _ := CacheDir()
	if got, want := cache, filepath.Join(home, ".cache", "murrly"); got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}

func TestDarwinConventions(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	home, _ := os.UserHomeDir()
	cfg, _ := ConfigDir()
	if got, want := cfg, filepath.Join(home, "Library", "Application Support", "Murrly"); got != want {
		t.Errorf("ConfigDir = %q, want %q", got, want)
	}
	data, _ := DataDir()
	if got, want := data, filepath.Join(home, "Library", "Application Support", "Murrly"); got != want {
		t.Errorf("DataDir = %q, want %q", got, want)
	}
	cache, _ := CacheDir()
	if got, want := cache, filepath.Join(home, "Library", "Caches", "Murrly"); got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}
