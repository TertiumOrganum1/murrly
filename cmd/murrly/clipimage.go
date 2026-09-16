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

// A picture the user had in the clipboard is kept, but the way back out is a
// file rather than the clipboard.
//
// That used to be forced on us by xclip, which served its payload for ANY
// target it was asked for: republish a PNG through it and every Ctrl+V of text
// anywhere on the desktop came back with PNG bytes in it. xclip is gone now
// and Murrly is a real selection owner that refuses targets it cannot produce
// (internal/clipboard/x11owner_linux.go), so putting a picture back would no
// longer poison anything.
//
// It is still a file, now by choice. Serving the image means holding megabytes
// as the live clipboard for the rest of the session, and answering INCR
// transfers for them, in aid of undoing one accident. A file is durable, is
// where the user's other screenshots already live, and survives Murrly
// exiting — which a selection never does.

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
