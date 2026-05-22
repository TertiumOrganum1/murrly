// gen-icons creates four 64x64 PNG icons used by the tray.
// Run once: `go run ./cmd/gen-icons`.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

func main() {
	icons := []struct {
		name string
		col  color.RGBA
	}{
		{"icon-idle.png", color.RGBA{120, 120, 120, 255}},
		{"icon-recording.png", color.RGBA{220, 40, 40, 255}},
		{"icon-transcribing.png", color.RGBA{220, 180, 40, 255}},
		{"icon-error.png", color.RGBA{180, 30, 30, 255}},
	}
	const outDir = "cmd/murrly/assets"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, ic := range icons {
		img := drawCircle(64, ic.col)
		path := filepath.Join(outDir, ic.name)
		f, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			log.Fatal(err)
		}
		f.Close()
		log.Printf("wrote %s", path)
	}
}

func drawCircle(size int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := size/2, size/2
	r := size/2 - 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
	return img
}
