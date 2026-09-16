//go:build !linux

package clipboard

// EnableSnapshot is the X11 kill switch elsewhere: macOS and Windows have a
// real clipboard store with no owning process, so reading it costs nothing and
// waits on nobody. The setting is accepted and ignored so one call site in
// main.go serves every platform.
func EnableSnapshot(bool) {}
