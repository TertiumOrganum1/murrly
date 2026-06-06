//go:build linux

// Package nemotronservice manages the Nemotron sidecar systemd user service
// (Linux only).
package nemotronservice

import "os/exec"

// Unit is the systemd --user unit name for the sidecar.
const Unit = "murrly-nemotron.service"

// Restart restarts the sidecar service. Used by the tray's "Перезапустить
// Nemotron" item when the sidecar wedges (CUDA hiccup, stuck decode).
func Restart() error {
	return exec.Command("systemctl", "--user", "restart", Unit).Run()
}
