//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

const appDisplayName = "Murrly"

// ConfigDir returns %AppData%\Murrly\ (roaming — config follows the user
// across machines, matching how Windows treats per-user settings).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir() // %AppData%
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDisplayName), nil
}

// DataDir returns %LocalAppData%\Murrly\ — the Whisper models live here
// (large, machine-local, must not roam).
func DataDir() (string, error) {
	base, err := localAppData()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDisplayName), nil
}

// CacheDir returns %LocalAppData%\Murrly\cache\ for the rotating log.
func CacheDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache"), nil
}

// localAppData resolves %LocalAppData% (os.UserCacheDir returns it on
// Windows), falling back to the home profile if the env var is unset.
func localAppData() (string, error) {
	if p := os.Getenv("LOCALAPPDATA"); p != "" {
		return p, nil
	}
	return os.UserCacheDir()
}
