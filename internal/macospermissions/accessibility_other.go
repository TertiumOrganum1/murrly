//go:build !darwin

// Package macospermissions wraps macOS Accessibility / TCC checks.
// On non-macOS platforms, all functions are no-ops returning true.
package macospermissions

// IsAccessibilityTrusted always returns true on non-macOS platforms.
// There is no equivalent permission outside macOS.
func IsAccessibilityTrusted() bool { return true }

// EnsureAccessibility is a no-op on non-macOS platforms.
func EnsureAccessibility() bool { return true }
