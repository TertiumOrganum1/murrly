// Package logfile provides a small rotating log file writer.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type RotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

func DefaultPath(appName string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, appName+".log"), nil
}

func Open(path string, maxBytes int64, backups int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("logfile: maxBytes must be positive")
	}
	if backups < 0 {
		return nil, fmt.Errorf("logfile: backups must be non-negative")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &RotatingFile{
		path:     path,
		maxBytes: maxBytes,
		backups:  backups,
		file:     f,
		size:     info.Size(),
	}, nil
}

func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		return 0, fmt.Errorf("logfile: closed")
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

func (r *RotatingFile) rotate() error {
	if err := r.file.Close(); err != nil {
		return err
	}
	r.file = nil

	if r.backups > 0 {
		for i := r.backups - 1; i >= 1; i-- {
			oldPath := fmt.Sprintf("%s.%d", r.path, i)
			newPath := fmt.Sprintf("%s.%d", r.path, i+1)
			_ = os.Remove(newPath)
			if _, err := os.Stat(oldPath); err == nil {
				if err := os.Rename(oldPath, newPath); err != nil {
					return err
				}
			}
		}
		firstBackup := r.path + ".1"
		_ = os.Remove(firstBackup)
		if _, err := os.Stat(r.path); err == nil {
			if err := os.Rename(r.path, firstBackup); err != nil {
				return err
			}
		}
	} else {
		_ = os.Remove(r.path)
	}

	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	r.file = f
	r.size = 0
	return nil
}
