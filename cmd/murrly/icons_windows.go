//go:build windows

package main

import "embed"

// Windows tray pack — the same colored cat poses as Linux, but as .ico files
// (generated from the Linux PNGs by cmd/gen-win-icons). fyne.io/systray loads
// the tray image via LoadImageW(IMAGE_ICON), which needs .ico, not PNG.

//go:embed assets/tray/windows
var iconFS embed.FS

const iconDir = "assets/tray/windows"

// iconExt is the embedded tray-icon extension on this platform.
const iconExt = ".ico"
