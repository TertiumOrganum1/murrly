//go:build linux

package paths

import (
	"os"
	"path/filepath"
)

const appName = "murrly"

// ConfigDir returns ~/.config/murrly/ (XDG_CONFIG_HOME aware).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName), nil
}

// DataDir returns ~/.local/share/murrly/ (XDG_DATA_HOME aware).
func DataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName), nil
}

// CacheDir returns ~/.cache/murrly/ (XDG_CACHE_HOME aware).
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName), nil
}
