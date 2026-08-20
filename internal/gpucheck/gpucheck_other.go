//go:build !linux

package gpucheck

// PreferDevice and EnsureFree are no-ops off Linux. macOS runs Whisper on
// Metal against unified memory: there is no second card to choose between and
// no separate VRAM budget to check, so neither the device-ordering trap nor
// the orphaned-context failure these guards exist for can arise. Windows
// builds do use CUDA, but ship without an nvidia-smi guarantee; wiring the
// probe there is a separate exercise.

// PreferDevice reports that no device pinning happened.
func PreferDevice(want string) (string, bool) { return "", false }

// EnsureFree always allows the load.
func EnsureFree(modelPath string) error { return nil }
