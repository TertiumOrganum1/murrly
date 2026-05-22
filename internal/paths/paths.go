// Package paths provides platform-correct user directories for Murrly.
package paths

import "path/filepath"

// ConfigFile returns the absolute path to config.toml inside ConfigDir.
func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// ModelsDir returns the absolute path to the directory where Whisper
// model files live (inside DataDir).
func ModelsDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models"), nil
}
