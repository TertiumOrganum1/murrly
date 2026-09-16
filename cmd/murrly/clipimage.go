package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tertiumorganum1/murrly/internal/clipboard"
)

// A picture the user had in the clipboard can be kept, but it can never go
// back into the clipboard — so the way out is a file.
//
// The reason is xclip: it serves its payload for ANY target it is asked for,
// ignoring what it advertised in TARGETS. Republish a PNG and every Ctrl+V of
// text anywhere on the desktop comes back with PNG bytes in it, which is worse
// than the image simply being gone (a well-behaved owner at least refuses the
// conversion). Doing it properly means Murrly becoming a real selection owner,
// INCR protocol and all, and owning the clipboard is the thing we spent this
// whole rewrite getting out of. A file on disk loses nothing and risks nothing.

// imageExtensions maps the X11 selection target (which is the MIME type) to
// the extension to save under. Anything unlisted keeps the subtype verbatim.
var imageExtensions = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
	"image/bmp":  "bmp",
	"image/tiff": "tiff",
}

func imageExtension(target string) string {
	if ext, ok := imageExtensions[strings.ToLower(target)]; ok {
		return ext
	}
	if _, sub, found := strings.Cut(target, "/"); found && sub != "" {
		return sub
	}
	return "bin"
}

// pictureDir is where the desktop keeps screenshots — the place the user will
// look for this file. xdg-user-dir knows the localised name ("Изображения"),
// so ask it rather than guessing; fall back to ~/Pictures and then to the home
// directory, both of which are better than failing to save at all.
func pictureDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	if out, err := exec.Command("xdg-user-dir", "PICTURES").Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" && dir != home {
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return dir
			}
		}
	}
	pictures := filepath.Join(home, "Pictures")
	if st, err := os.Stat(pictures); err == nil && st.IsDir() {
		return pictures
	}
	return home
}

// saveClipboardImage writes the stashed picture to a dated file and returns
// the path it landed on. The timestamp makes the name unique, so saving twice
// never silently overwrites the first one.
func saveClipboardImage(s clipboard.Saved) (string, error) {
	if !s.HasImage() {
		return "", fmt.Errorf("в снимке буфера нет картинки")
	}
	name := fmt.Sprintf("murrly-clipboard-%s.%s",
		time.Now().Format("20060102-150405"), imageExtension(s.ImageTarget))
	path := filepath.Join(pictureDir(), name)
	if err := os.WriteFile(path, s.Image, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
