//go:build !darwin

package main

// Linux/Windows have no TCC-style per-pane settings deep link, and our
// hotkey/paste/mic paths don't gate on user-revocable permissions.
// Privacy menu entries are hidden when this returns false.
func openPrivacyPane(string)         {}
func privacyPanesSupported() bool    { return false }
