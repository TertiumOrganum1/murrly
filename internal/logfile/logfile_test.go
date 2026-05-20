package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatesBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice-input.log")
	log, err := Open(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if _, err := log.Write([]byte("123456789\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("abcdef\n")); err != nil {
		t.Fatal(err)
	}

	current := mustRead(t, path)
	if current != "abcdef\n" {
		t.Fatalf("current log = %q", current)
	}
	backup := mustRead(t, path+".1")
	if backup != "123456789\n" {
		t.Fatalf("backup log = %q", backup)
	}
}

func TestKeepsConfiguredBackupCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice-input.log")
	log, err := Open(path, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	for _, line := range []string{"aa\n", "bb\n", "cc\n", "dd\n"} {
		if _, err := log.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}

	if got := mustRead(t, path); got != "dd\n" {
		t.Fatalf("current log = %q", got)
	}
	if got := mustRead(t, path+".1"); got != "cc\n" {
		t.Fatalf("first backup = %q", got)
	}
	if got := mustRead(t, path+".2"); got != "bb\n" {
		t.Fatalf("second backup = %q", got)
	}
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatalf("unexpected third backup")
	}
}

func TestDefaultPathUsesUserCacheDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	path, err := DefaultPath("voice-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join("voice-input", "voice-input.log")) {
		t.Fatalf("path = %q", path)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
