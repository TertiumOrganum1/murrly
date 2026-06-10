//go:build linux

// Package a11ysetup checks and applies the system prerequisites for
// context-aware insertion (internal/uicontext) on Linux:
//
//  1. the desktop's toolkit-accessibility gsettings key — Qt apps
//     (konsole, doublecmd, …) start their AT-SPI bridge only when
//     org.a11y.Status.IsEnabled is true at app launch, and the desktop
//     daemon keeps that bus property synced to this key;
//  2. VS Code's editor.accessibilitySupport=on — Chromium/Electron
//     keeps its accessibility tree off until told a screen reader
//     exists, and this setting is the supported per-app way to force
//     it (the global ScreenReaderEnabled flag is NOT used: Mint
//     auto-launches the Orca screen reader on it).
//
// GTK apps and Telegram Desktop need nothing. The tray's "Контекстная
// вставка" item surfaces Check/Apply to the user.
package a11ysetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Status reports which prerequisites are in place.
type Status struct {
	// ToolkitA11y — the gsettings toolkit-accessibility key is on.
	ToolkitA11y bool
	// VSCodeFound — a VS Code user profile exists on this machine.
	VSCodeFound bool
	// VSCode — editor.accessibilitySupport is "on" in its settings.
	// Meaningless when VSCodeFound is false.
	VSCode bool
}

// Ready is true when everything that exists on this machine is set up.
func (s Status) Ready() bool {
	return s.ToolkitA11y && (!s.VSCodeFound || s.VSCode)
}

// gsettingsSchemas lists the desktops' interface schemas that carry
// toolkit-accessibility, most specific first (Mint/Cinnamon syncs the
// a11y bus from its own schema).
var gsettingsSchemas = []string{
	"org.cinnamon.desktop.interface",
	"org.gnome.desktop.interface",
}

const a11yKey = "toolkit-accessibility"

// Check inspects the current state without changing anything.
func Check() Status {
	var st Status
	for _, schema := range gsettingsSchemas {
		out, err := exec.Command("gsettings", "get", schema, a11yKey).Output()
		if err != nil {
			continue
		}
		st.ToolkitA11y = strings.TrimSpace(string(out)) == "true"
		break
	}
	path, err := vscodeSettingsPath()
	if err == nil && path != "" {
		st.VSCodeFound = true
		if raw, rerr := os.ReadFile(path); rerr == nil {
			st.VSCode = vscodeAccessibilityOn(raw)
		}
		// A profile dir without settings.json still counts as found —
		// Apply will create the file.
	}
	return st
}

// Apply turns on whatever Check found missing. Returns the resulting
// status plus human-readable lines describing what was done / what
// needs a manual step (shown as a desktop notification).
func Apply() (Status, []string, error) {
	st := Check()
	var msgs []string
	var firstErr error

	if !st.ToolkitA11y {
		applied := false
		for _, schema := range gsettingsSchemas {
			if err := exec.Command("gsettings", "set", schema, a11yKey, "true").Run(); err != nil {
				continue
			}
			// set is silent even for unknown schemas in some builds —
			// read back to be sure.
			out, err := exec.Command("gsettings", "get", schema, a11yKey).Output()
			if err == nil && strings.TrimSpace(string(out)) == "true" {
				applied = true
				break
			}
		}
		if applied {
			st.ToolkitA11y = true
			msgs = append(msgs, "Системная доступность включена (Qt-приложения подхватят после их перезапуска).")
		} else {
			firstErr = fmt.Errorf("gsettings %s не применился", a11yKey)
			msgs = append(msgs, "Не удалось включить toolkit-accessibility через gsettings.")
		}
	}

	if st.VSCodeFound && !st.VSCode {
		if err := patchVSCodeSettingsFile(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			msgs = append(msgs, "VS Code: не получилось обновить settings.json — добавьте вручную: \"editor.accessibilitySupport\": \"on\".")
		} else {
			st.VSCode = true
			msgs = append(msgs, "VS Code настроен (нужен его перезапуск).")
		}
	}

	if len(msgs) == 0 {
		msgs = append(msgs, "Всё уже настроено.")
	}
	return st, msgs, firstErr
}

// vscodeSettingsPath returns the user settings.json of the standard
// VS Code install, or "" when no profile dir exists.
func vscodeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "Code", "User")
	if _, err := os.Stat(dir); err != nil {
		return "", nil
	}
	return filepath.Join(dir, "settings.json"), nil
}

var accessibilityRe = regexp.MustCompile(`("editor\.accessibilitySupport"\s*:\s*)"[^"]*"`)

// vscodeAccessibilityOn reports whether settings.json already forces
// accessibility on.
func vscodeAccessibilityOn(src []byte) bool {
	m := accessibilityRe.Find(src)
	return m != nil && strings.HasSuffix(string(m), `"on"`)
}

// patchVSCodeSettingsFile rewrites settings.json so that
// editor.accessibilitySupport is "on", preserving the rest of the file
// byte-for-byte (VS Code's settings are JSONC — comments and trailing
// commas are legal, so a parse/re-marshal round-trip is NOT safe).
// The previous content is kept next to it as settings.json.murrly-bak.
func patchVSCodeSettingsFile() error {
	path, err := vscodeSettingsPath()
	if err != nil || path == "" {
		return fmt.Errorf("vscode settings path: %w", err)
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
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

// patchVSCodeSettings is the pure (testable) patch body: replace the
// key's value when present, otherwise insert the key right after the
// opening brace.
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
