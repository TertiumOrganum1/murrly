//go:build linux

// Package autostart toggles "launch at login" for Murrly. On Linux this
// is a .desktop file in the standard XDG autostart directory; the
// freedesktop session daemon picks it up.
package autostart

import (
	"os"
	"path/filepath"
)

func autostartPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "autostart", "murrly.desktop")
}

func Enabled() bool {
	p := autostartPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// Enable copies the system .desktop file into the autostart directory
// (or creates a minimal one referencing the installed binary). Murrly's
// `make autostart` is the canonical creator; this is a fallback for
// menu-driven toggles.
func Enable() error {
	p := autostartPath()
	if p == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// Look for the system-installed launcher and copy it.
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	src := filepath.Join(home, ".local", "share", "applications", "murrly.desktop")
	data, err := os.ReadFile(src)
	if err != nil {
		// Fall back to a minimal entry if the system one isn't installed yet.
		minimal := []byte(`[Desktop Entry]
Type=Application
Name=Murrly
Exec=` + filepath.Join(home, ".local", "bin", "murrly") + `
X-GNOME-Autostart-enabled=true
`)
		return os.WriteFile(p, minimal, 0o644)
	}
	return os.WriteFile(p, data, 0o644)
}

func Disable() error {
	p := autostartPath()
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
