//go:build windows

// Package autostart toggles "launch at login" for Murrly. On Windows this
// is a value under HKCU\Software\Microsoft\Windows\CurrentVersion\Run — the
// shell starts every command listed there when the user signs in. Per-user
// (HKCU, not HKLM) so it needs no admin rights.
package autostart

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	// valueName is the Run entry's name; the data is the exe path. Must match
	// across Enable/Disable/Enabled so we toggle the same entry.
	valueName = "Murrly"
)

// Enabled reports whether the Run entry exists (regardless of which path it
// points at — a stale path from a previous install location still counts as
// "on", and Enable would refresh it).
func Enabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(valueName)
	return err == nil
}

// Enable writes the current executable's path into the Run key, quoted so a
// path with spaces (Program Files, a profile name) is parsed as one argument.
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(valueName, `"`+exe+`"`)
}

// Disable removes the Run entry. Missing entry is success (idempotent).
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}
