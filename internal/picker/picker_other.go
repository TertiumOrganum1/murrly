//go:build !linux && !darwin

package picker

// Pick is a no-op on platforms without the spawned Fyne picker (anything
// that isn't Linux or macOS). The real spawn driver lives in
// picker_spawn.go for linux || darwin.
func Pick(text string, options []string) (int, bool) {
	return 0, false
}
