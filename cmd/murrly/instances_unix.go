//go:build linux || darwin

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// terminateOtherInstances sends SIGTERM to any other running Murrly process
// (same executable basename) and waits up to ~3s for each to exit, so its
// GPU/CUDA context is torn down cleanly before this instance loads its model.
// SIGTERM, never SIGKILL: a hard kill mid-cudaMalloc is exactly what leaks an
// unreclaimable context. A wedged instance that ignores SIGTERM is left alone
// (logged) rather than force-killed.
func terminateOtherInstances() {
	self := os.Getpid()
	exe, err := os.Executable()
	if err != nil {
		return
	}
	name := filepath.Base(exe)
	out, err := exec.Command("pgrep", "-x", name).Output()
	if err != nil {
		return // none found, or pgrep unavailable
	}
	var others []int
	for _, f := range strings.Fields(string(out)) {
		pid, perr := strconv.Atoi(f)
		if perr != nil || pid == self {
			continue
		}
		if syscall.Kill(pid, syscall.SIGTERM) == nil {
			others = append(others, pid)
		}
	}
	if len(others) == 0 {
		return
	}
	log.Printf("startup: SIGTERM to stale Murrly instance(s) %v, waiting for clean exit", others)
	for _, pid := range others {
		for i := 0; i < 30; i++ { // up to ~3s per instance
			if syscall.Kill(pid, 0) != nil {
				break // gone
			}
			time.Sleep(100 * time.Millisecond)
		}
		if syscall.Kill(pid, 0) == nil {
			log.Printf("startup: instance %d still alive after SIGTERM — proceeding anyway", pid)
		}
	}
}
