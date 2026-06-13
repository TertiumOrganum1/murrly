//go:build linux || darwin || windows

// Package picker shows a single-select chooser so the user can pick among
// multi-inference variants. On Linux, macOS and Windows it spawns the
// standalone Fyne binary (murrly-picker): options go in on stdin
// (NUL-separated, since a variant may span several lines), the chosen
// 0-based index comes back on stdout. The spawn + read logic here is
// platform-neutral; the binary handles its own placement (X11 taskbar-skip +
// centring on Linux, Fyne's default centred frontmost window elsewhere), so
// this driver is just spawn + read everywhere.
package picker

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Pick spawns the picker binary over options and returns the 0-based
// index of the chosen one. ok=false when the user cancels (Esc / close)
// or the picker binary can't be found/launched. text is the prompt the
// caller would show; the Fyne binary draws no header, so it's unused here
// but kept for the cross-platform signature.
func Pick(text string, options []string) (int, bool) {
	_ = text
	if len(options) == 0 {
		return 0, false
	}
	bin := pickerBinary()
	if bin == "" {
		return 0, false
	}

	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(strings.Join(options, "\x00"))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// Non-zero exit = user cancelled / closed the window.
		return 0, false
	}

	n, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil || n < 0 || n >= len(options) {
		return 0, false
	}
	return n, true
}

// exeName appends Windows' .exe extension to a base executable name.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// pickerBinary locates the murrly-picker executable. It's installed
// alongside murrly itself, so the sibling of the running binary is the
// first and normal hit; PATH and the dev build dir are fallbacks.
func pickerBinary() string {
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), exeName("murrly-picker"))
		if isExec(sibling) {
			return sibling
		}
	}
	if p, err := exec.LookPath("murrly-picker"); err == nil {
		return p
	}
	if dev := filepath.Join("bin", exeName("picker")); isExec(dev) { // dev: `go build -o bin/picker ./cmd/picker`
		return dev
	}
	return ""
}

// isExec reports whether path is a runnable file. On Windows the Unix
// executable bit is meaningless (Stat reports it inconsistently), so we only
// require a regular file; the .exe extension is what makes it runnable there.
func isExec(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
