//go:build !linux && !darwin && !windows

package picker

// Pick is a no-op on platforms without the spawned Fyne picker (anything
// that isn't Linux, macOS or Windows). The real spawn driver lives in
// picker_spawn.go for linux || darwin || windows.
func Pick(text string, options []string) (int, bool) {
	return 0, false
}
