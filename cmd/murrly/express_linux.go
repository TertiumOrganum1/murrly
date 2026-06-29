//go:build linux

package main

import (
	"log"
	"strings"
	"time"

	"github.com/tertiumorganum1/murrly/internal/a11ysetup"
	"github.com/tertiumorganum1/murrly/internal/menuactions"
)

// expressAutoManage is OFF by design: the user launches eXpress manually with
// the flag, so Murrly neither restarts it at startup nor shows the tray item.
// The machinery (a11ysetup.RestartExpressWithA11y / EnsureExpressA11y and the
// body below) is kept intentionally — flip this to true to re-enable.
const expressAutoManage = false

// wireExpressSetup wires the tray "relaunch eXpress with accessibility" action
// and, at startup, replaces a bare login-autostarted eXpress with a flagged
// one. eXpress regenerates its own autostart .desktop from in-app settings, so
// patching launcher files never sticks — having Murrly relaunch it with
// --force-renderer-accessibility is the reliable lever. Currently gated off via
// expressAutoManage; also a no-op when eXpress isn't installed.
func wireExpressSetup(actions *menuactions.Actions) {
	if !expressAutoManage || !a11ysetup.ExpressInstalled() {
		return
	}
	actions.OnRestartExpress = func() {
		msgs, err := a11ysetup.RestartExpressWithA11y()
		if err != nil {
			log.Printf("eXpress restart: %v", err)
		}
		if len(msgs) > 0 {
			desktopNotify("Murrly: eXpress", strings.Join(msgs, "\n"))
		}
	}
	// At Murrly startup, give a login-autostarted eXpress a few seconds to come
	// up, then replace it with a flagged instance if it's running bare. Skips
	// an already-flagged instance, so a normal Murrly restart won't disturb it.
	go func() {
		time.Sleep(6 * time.Second)
		a11ysetup.EnsureExpressA11y()
	}()
}
