//go:build !darwin

package main

// setupMetalResources is a no-op on non-macOS platforms.
func setupMetalResources() {}
