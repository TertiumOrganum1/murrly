//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// terminateOtherInstances kills any other running Murrly process (same exe
// basename) before this one loads its model, so two instances don't fight
// over the GPU. Unlike the Linux SIGTERM path, a hard TerminateProcess is
// fine here: the Windows display driver reclaims a dead process's VRAM and
// CUDA context on exit, so there's no leaked-context hazard to tiptoe around.
func terminateOtherInstances() {
	self := uint32(os.Getpid())
	exe, err := os.Executable()
	if err != nil {
		return
	}
	name := filepath.Base(exe) // "murrly.exe"

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var others []uint32
	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if pe.ProcessID == self {
			continue
		}
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), name) {
			others = append(others, pe.ProcessID)
		}
	}

	for _, pid := range others {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
		if err != nil {
			continue
		}
		if err := windows.TerminateProcess(h, 0); err == nil {
			windows.WaitForSingleObject(h, 3000) // up to 3s for the process to go away
			log.Printf("startup: terminated stale Murrly instance %d", pid)
		}
		windows.CloseHandle(h)
	}
}
