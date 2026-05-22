//go:build darwin

package paths

import (
	"os"
	"path/filepath"
)

const appDisplayName = "Murrly"

// ConfigDir returns ~/Library/Application Support/Murrly/.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDisplayName), nil
}

// DataDir returns ~/Library/Application Support/Murrly/ (same as ConfigDir
// on macOS — Apple's HIG combines both concepts).
func DataDir() (string, error) {
	return ConfigDir()
}

// CacheDir returns ~/Library/Caches/Murrly/.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDisplayName), nil
}
