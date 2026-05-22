//go:build !darwin

package main

// logAppName follows the lowercase XDG convention on Linux:
// ~/.cache/murrly/.
func logAppName() string { return "murrly" }
