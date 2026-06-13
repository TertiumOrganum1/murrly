//go:build windows

// Package a11ysetup on Windows handles the one prerequisite that context-aware
// insertion (internal/uicontext, via UI Automation) needs and that isn't on by
// default: VS Code's editor.accessibilitySupport=on. Chromium/Electron keeps
// its UIA accessibility tree off until it believes a screen reader is present,
// so without this setting VS Code (and other Electron apps) expose no text
// pattern and the caret context can't be read. Native Win32 fields (Notepad,
// WordPad, Office, most browsers' address bars) need nothing. The tray's
// "Контекстная вставка" item surfaces Check/Apply.
package a11ysetup

import (
	"fmt"
	"os"
	"path/filepath"
)

// Status reports which prerequisites are in place. ToolkitA11y has no Windows
// analogue (it's the Linux gsettings key) and is always true here, so Ready
// turns purely on the VS Code setting.
type Status struct {
	ToolkitA11y bool
	VSCodeFound bool
	VSCode      bool
}

// Ready is true when everything that exists on this machine is set up.
func (s Status) Ready() bool {
	return !s.VSCodeFound || s.VSCode
}

// Check inspects the current state without changing anything.
func Check() Status {
	st := Status{ToolkitA11y: true}
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

// Apply turns on whatever Check found missing (VS Code only on Windows) and
// returns human-readable lines for a desktop notification.
func Apply() (Status, []string, error) {
	st := Check()
	var msgs []string
	var firstErr error

	if st.VSCodeFound && !st.VSCode {
		if err := patchVSCodeSettingsFile(); err != nil {
			firstErr = err
			msgs = append(msgs, "VS Code: не получилось обновить settings.json — добавьте вручную: \"editor.accessibilitySupport\": \"on\".")
		} else {
			st.VSCode = true
			msgs = append(msgs, "VS Code настроен (нужен его перезапуск).")
		}
	}

	if len(msgs) == 0 {
		msgs = append(msgs, "Всё настроено. Нативные поля (Блокнот, Word, браузеры) работают через UI Automation без настройки; Electron-приложения подхватят после перезапуска.")
	}
	return st, msgs, firstErr
}

// vscodeSettingsPath returns the user settings.json of the standard VS Code
// install (%AppData%\Code\User\settings.json), or "" when no profile exists.
func vscodeSettingsPath() (string, error) {
	base := os.Getenv("APPDATA")
	if base == "" {
		var err error
		if base, err = os.UserConfigDir(); err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "Code", "User")
	if _, err := os.Stat(dir); err != nil {
		return "", nil
	}
	return filepath.Join(dir, "settings.json"), nil
}

// patchVSCodeSettingsFile forces editor.accessibilitySupport on (shared patch
// logic in a11ysetup_vscode.go).
func patchVSCodeSettingsFile() error {
	path, err := vscodeSettingsPath()
	if err != nil || path == "" {
		return fmt.Errorf("vscode settings path: %w", err)
	}
	return patchVSCodeFileAt(path)
}
