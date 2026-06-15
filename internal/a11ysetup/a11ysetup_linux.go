//go:build linux

// Package a11ysetup checks and applies the system prerequisites for
// context-aware insertion (internal/uicontext) on Linux:
//
//  1. the desktop's toolkit-accessibility gsettings key — Qt apps
//     (konsole, doublecmd, …) start their AT-SPI bridge only when
//     org.a11y.Status.IsEnabled is true at app launch, and the desktop
//     daemon keeps that bus property synced to this key.
//
// We deliberately do NOT touch VS Code's editor.accessibilitySupport on
// Linux: turning it on makes the Claude Code chat input expose an
// unreliable AT-SPI tree (a placeholder embed plus a second, text-bearing
// focused entry), so Capture reads the wrong element and mangles inserts.
// With the setting off the chat is a single opaque placeholder, which the
// uicontext detector cleanly treats as "unreadable" → passthrough (correct
// capital + terminator). GTK apps, Qt apps and Telegram Desktop work via
// the gsettings flag alone. The tray's "Контекстная вставка" item surfaces
// Check/Apply to the user.
package a11ysetup

import (
	"fmt"
	"os/exec"
	"strings"
)

// Status reports which prerequisites are in place. VSCodeFound/VSCode are
// retained for the cross-platform struct shape (Windows uses them) but stay
// false on Linux — we no longer manage the VS Code setting here.
type Status struct {
	// ToolkitA11y — the gsettings toolkit-accessibility key is on.
	ToolkitA11y bool
	// VSCodeFound — a VS Code user profile exists (unused on Linux).
	VSCodeFound bool
	// VSCode — editor.accessibilitySupport is "on" (unused on Linux).
	VSCode bool
}

// Ready is true when everything Linux manages (the gsettings flag) is set.
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
	// VS Code is intentionally NOT probed/managed on Linux — see the package
	// doc. VSCodeFound stays false so Ready() keys off the gsettings flag only.
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

	if len(msgs) == 0 {
		msgs = append(msgs, "Всё уже настроено.")
	}
	return st, msgs, firstErr
}
