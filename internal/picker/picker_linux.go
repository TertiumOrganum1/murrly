//go:build linux

// Package picker shows a single-select chooser so the user can pick among
// multi-inference variants. On Linux it spawns the standalone Fyne binary
// (murrly-picker): options go in on stdin (NUL-separated, since a variant
// may span several lines), the chosen 0-based index comes back on stdout.
// The binary handles its own placement (taskbar-skip + centring on the
// monitor under the mouse), so this driver is just spawn + read.
package picker

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

// pickerBinary locates the murrly-picker executable. It's installed
// alongside murrly itself, so the sibling of the running binary is the
// first and normal hit; PATH and the dev build dir are fallbacks.
func pickerBinary() string {
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "murrly-picker")
		if isExec(sibling) {
			return sibling
		}
	}
	if p, err := exec.LookPath("murrly-picker"); err == nil {
		return p
	}
	if isExec("bin/picker") { // dev: `go build -o bin/picker ./cmd/picker`
		return "bin/picker"
	}
	return ""
}

func isExec(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
