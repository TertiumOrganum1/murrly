//go:build linux

// Package a11ysetup checks and applies the system prerequisites for
// context-aware insertion (internal/uicontext) on Linux:
//
//  1. the desktop's toolkit-accessibility gsettings key — Qt apps
//     (konsole, doublecmd, …) start their AT-SPI bridge only when
//     org.a11y.Status.IsEnabled is true at app launch, and the desktop
//     daemon keeps that bus property synced to this key.
//
// We deliberately do NOT touch VS Code's editor.accessibilitySupport on
// Linux: turning it on makes the Claude Code chat input expose an
// unreliable AT-SPI tree (a placeholder embed plus a second, text-bearing
// focused entry), so Capture reads the wrong element and mangles inserts.
// With the setting off the chat is a single opaque placeholder, which the
// uicontext detector cleanly treats as "unreadable" → passthrough (correct
// capital + terminator). GTK apps, Qt apps and Telegram Desktop work via
// the gsettings flag alone. The tray's "Контекстная вставка" item surfaces
// Check/Apply to the user.
package a11ysetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// eXpress (corporate Electron messenger) exposes NO AT-SPI tree for its
// renderer unless launched with --force-renderer-accessibility, so its message
// field is unreadable and inserts there can't adapt to context. eXpress
// rewrites its own autostart .desktop from in-app settings on every launch, so
// patching launcher files never sticks. Instead Murrly relaunches eXpress with
// the flag — on demand (tray item) and at Murrly startup if a bare instance is
// running (EnsureExpressA11y).
const (
	expressBin  = "/opt/eXpress/express"
	expressFlag = "--force-renderer-accessibility"
)

// ExpressInstalled reports whether the eXpress binary is present.
func ExpressInstalled() bool {
	_, err := os.Stat(expressBin)
	return err == nil
}

// expressState scans /proc for the main eXpress process (its argv0 is the
// binary; helper processes carry --type=) and reports whether it's running and
// whether that launch carried the accessibility flag.
func expressState() (running, withFlag bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if len(args) == 0 || args[0] != expressBin {
			continue
		}
		helper := false
		for _, a := range args {
			if strings.HasPrefix(a, "--type=") {
				helper = true
			}
		}
		if helper {
			continue // renderer/zygote/gpu child, not the main process
		}
		running = true
		for _, a := range args {
			if a == expressFlag {
				return true, true
			}
		}
		return true, false
	}
	return false, false
}

// ExpressReady reports whether the running eXpress already has the flag (so the
// tray status can show it). True (nothing to do) when eXpress isn't installed.
func ExpressReady() bool {
	if !ExpressInstalled() {
		return true
	}
	_, withFlag := expressState()
	return withFlag
}

// RestartExpressWithA11y kills any running eXpress and relaunches it DETACHED
// with --force-renderer-accessibility. eXpress regenerates its own autostart
// entry from in-app settings, so patching .desktop files never sticks — having
// Murrly relaunch it is the reliable lever. No-op when eXpress isn't installed.
func RestartExpressWithA11y() ([]string, error) {
	if !ExpressInstalled() {
		return []string{"eXpress не установлен."}, nil
	}
	_ = exec.Command("pkill", "-9", "-f", expressBin).Run() // ignore "no process"
	// Wait for the main process to die, then clear eXpress's stale single-
	// instance lock. A -9'd or orphaned process leaves SingletonLock pointing at
	// a dead PID, and the fresh launch exits on sight of it — the "kills but
	// won't relaunch" bug. Electron recreates these files. Order matches the
	// working manual recovery: kill all → remove lock → relaunch.
	for i := 0; i < 25; i++ {
		if running, _ := expressState(); !running {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".config", "eXpress")
		for _, f := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
			_ = os.Remove(filepath.Join(dir, f))
		}
	}
	if err := launchExpressDetached(); err != nil {
		return []string{"Не удалось запустить eXpress с флагом доступности."}, err
	}
	return []string{"eXpress перезапущен с флагом доступности."}, nil
}

// EnsureExpressA11y restarts eXpress with the flag only if it's currently
// running WITHOUT it — called at Murrly startup so a login-autostarted (bare)
// eXpress gets replaced by a flagged one, without disturbing an already-flagged
// instance or force-launching eXpress when the user hasn't opened it.
func EnsureExpressA11y() {
	if !ExpressInstalled() {
		return
	}
	if running, withFlag := expressState(); running && !withFlag {
		_, _ = RestartExpressWithA11y()
	}
}

// launchExpressDetached starts eXpress in its own session so it outlives
// Murrly, mirroring scripts/start-linux.sh's setsid use.
func launchExpressDetached() error {
	if path, err := exec.LookPath("setsid"); err == nil {
		return exec.Command(path, "-f", expressBin, expressFlag).Start()
	}
	cmd := exec.Command(expressBin, expressFlag)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Status reports which prerequisites are in place. VSCodeFound/VSCode are
// retained for the cross-platform struct shape (Windows uses them) but stay
// false on Linux — we no longer manage the VS Code setting here.
type Status struct {
	// ToolkitA11y — the gsettings toolkit-accessibility key is on.
	ToolkitA11y bool
	// VSCodeFound — a VS Code user profile exists (unused on Linux).
	VSCodeFound bool
	// VSCode — editor.accessibilitySupport is "on" (unused on Linux).
	VSCode bool
}

// Ready is true when everything Linux manages (the gsettings flag) is set.
func (s Status) Ready() bool {
	return s.ToolkitA11y && (!s.VSCodeFound || s.VSCode)
}

// gsettingsSchemas lists the desktops' interface schemas that carry
// toolkit-accessibility, most specific first (Mint/Cinnamon syncs the
// a11y bus from its own schema).
var gsettingsSchemas = []string{
	"org.cinnamon.desktop.interface",
	"org.gnome.desktop.interface",
}

const a11yKey = "toolkit-accessibility"

// Check inspects the current state without changing anything.
func Check() Status {
	var st Status
	for _, schema := range gsettingsSchemas {
		out, err := exec.Command("gsettings", "get", schema, a11yKey).Output()
		if err != nil {
			continue
		}
		st.ToolkitA11y = strings.TrimSpace(string(out)) == "true"
		break
	}
	// VS Code is intentionally NOT probed/managed on Linux — see the package
	// doc. VSCodeFound stays false so Ready() keys off the gsettings flag only.
	return st
}

// Apply turns on whatever Check found missing. Returns the resulting
// status plus human-readable lines describing what was done / what
// needs a manual step (shown as a desktop notification).
func Apply() (Status, []string, error) {
	st := Check()
	var msgs []string
	var firstErr error

	if !st.ToolkitA11y {
		applied := false
		for _, schema := range gsettingsSchemas {
			if err := exec.Command("gsettings", "set", schema, a11yKey, "true").Run(); err != nil {
				continue
			}
			// set is silent even for unknown schemas in some builds —
			// read back to be sure.
			out, err := exec.Command("gsettings", "get", schema, a11yKey).Output()
			if err == nil && strings.TrimSpace(string(out)) == "true" {
				applied = true
				break
			}
		}
		if applied {
			st.ToolkitA11y = true
			msgs = append(msgs, "Системная доступность включена (Qt-приложения подхватят после их перезапуска).")
		} else {
			firstErr = fmt.Errorf("gsettings %s не применился", a11yKey)
			msgs = append(msgs, "Не удалось включить toolkit-accessibility через gsettings.")
		}
	}

	if len(msgs) == 0 {
		msgs = append(msgs, "Всё уже настроено.")
	}
	return st, msgs, firstErr
}
