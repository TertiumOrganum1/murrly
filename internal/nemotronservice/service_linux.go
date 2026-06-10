//go:build linux

// Package nemotronservice manages the Nemotron sidecar systemd user service
// (Linux only).
package nemotronservice

import "os/exec"

// Unit is the systemd --user unit name for the sidecar.
const Unit = "murrly-nemotron.service"

// Restart restarts the sidecar service. Used by the tray's "Перезапустить
// Nemotron" item when the sidecar wedges (CUDA hiccup, stuck decode), and by
// the recognition path to revive a sidecar that systemd gave up on.
func Restart() error {
	return exec.Command("systemctl", "--user", "restart", Unit).Run()
}

// IsActive reports whether the service is running (or starting up). False
// means it failed / was stopped — i.e. systemd's retries were exhausted, so
// a recognition attempt should kick a fresh restart. True (active/activating)
// means it's up or still loading the model — leave it alone.
func IsActive() bool {
	return exec.Command("systemctl", "--user", "is-active", "--quiet", Unit).Run() == nil
}

// Enable starts the sidecar now and marks it for autostart at login.
// Driven by the tray "Движок Nemotron" toggle when the user opts in.
func Enable() error {
	return exec.Command("systemctl", "--user", "enable", "--now", Unit).Run()
}

// Disable stops the sidecar now and removes it from autostart, freeing its
// GPU memory. Driven by the same toggle when the user opts out (the default).
func Disable() error {
	return exec.Command("systemctl", "--user", "disable", "--now", Unit).Run()
}
