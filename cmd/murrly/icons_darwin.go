//go:build darwin

package main

import "embed"

// macOS tray pack — monochrome silhouettes. The menu bar style on
// macOS expects a single-color glyph that the system tints to match
// dark/light mode; colored icons stick out next to every other native
// menu-bar app and look out of place.

//go:embed assets/tray/darwin
var iconFS embed.FS

const iconDir = "assets/tray/darwin"
