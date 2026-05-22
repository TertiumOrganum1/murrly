//go:build darwin

package main

import (
	"log"
	"os/exec"
)

// openPrivacyPane opens System Settings to a specific Privacy & Security
// pane. The x-apple.systempreferences:com.apple.preference.security URL
// is a documented LaunchServices hook; `Privacy_Microphone`,
// `Privacy_Accessibility`, etc. are the pane anchors macOS exposes.
//
// We use this because AXIsProcessTrustedWithOptions only flashes a brief
// system toast — easy to miss. A menu item that lands directly in the
// right pane is much more discoverable.
func openPrivacyPane(pane string) {
	url := "x-apple.systempreferences:com.apple.preference.security?Privacy_" + pane
	if err := exec.Command("open", url).Start(); err != nil {
		log.Printf("open privacy pane %s: %v", pane, err)
	}
}

func privacyPanesSupported() bool { return true }
