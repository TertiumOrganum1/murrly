// Shared (platform-agnostic) helpers for forcing VS Code's
// editor.accessibilitySupport on. Chromium/Electron keeps its accessibility
// tree off until it believes a screen reader is present, and this per-app
// setting is the supported way to force it — needed for context-aware
// insertion to read VS Code (and other Electron) fields on every OS. The
// per-platform files supply the settings.json location; the patching itself
// is identical everywhere (the file is JSONC, so we splice rather than
// parse/re-marshal, preserving comments and trailing commas).
package a11ysetup

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var accessibilityRe = regexp.MustCompile(`("editor\.accessibilitySupport"\s*:\s*)"[^"]*"`)

// vscodeAccessibilityOn reports whether settings.json already forces
// accessibility on.
func vscodeAccessibilityOn(src []byte) bool {
	m := accessibilityRe.Find(src)
	return m != nil && strings.HasSuffix(string(m), `"on"`)
}

// patchVSCodeFileAt rewrites settings.json at path so that
// editor.accessibilitySupport is "on", preserving the rest byte-for-byte.
// The previous content is kept next to it as settings.json.murrly-bak.
func patchVSCodeFileAt(path string) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(path, []byte("{\n    \"editor.accessibilitySupport\": \"on\"\n}\n"), 0o644)
	}
	if err != nil {
		return err
	}
	patched, changed, perr := patchVSCodeSettings(raw)
	if perr != nil {
		return perr
	}
	if !changed {
		return nil
	}
	if err := os.WriteFile(path+".murrly-bak", raw, 0o644); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return os.WriteFile(path, patched, 0o644)
}

// patchVSCodeSettings is the pure (testable) patch body: replace the key's
// value when present, otherwise insert the key right after the opening brace.
func patchVSCodeSettings(src []byte) ([]byte, bool, error) {
	if accessibilityRe.Match(src) {
		out := accessibilityRe.ReplaceAll(src, []byte(`${1}"on"`))
		return out, string(out) != string(src), nil
	}
	if strings.TrimSpace(string(src)) == "" {
		return []byte("{\n    \"editor.accessibilitySupport\": \"on\"\n}\n"), true, nil
	}
	brace := strings.Index(string(src), "{")
	if brace < 0 {
		return nil, false, fmt.Errorf("settings.json: не найден объект настроек")
	}
	rest := strings.TrimSpace(string(src[brace+1:]))
	insert := "\n    \"editor.accessibilitySupport\": \"on\","
	if strings.HasPrefix(rest, "}") {
		insert = "\n    \"editor.accessibilitySupport\": \"on\"\n"
	}
	out := string(src[:brace+1]) + insert + string(src[brace+1:])
	return []byte(out), true, nil
}
