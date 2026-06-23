//go:build !linux

package main

import "github.com/tertiumorganum1/murrly/internal/menuactions"

// wireExpressSetup is a no-op off Linux: the eXpress relaunch-with-accessibility
// lever is Linux/AT-SPI specific.
func wireExpressSetup(actions *menuactions.Actions) {}
