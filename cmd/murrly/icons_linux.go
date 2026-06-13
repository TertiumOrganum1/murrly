//go:build linux

package main

import "embed"

// Linux tray pack — colored cat poses (cat_happy / listening /
// processing / cat_error). Cinnamon/MATE/GNOME panels render full-
// color RGBA icons fine, so we use the same artwork as the launcher.

//go:embed assets/tray/linux
var iconFS embed.FS

const iconDir = "assets/tray/linux"

// iconExt is the embedded tray-icon extension on this platform.
const iconExt = ".png"
