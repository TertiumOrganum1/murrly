// gen-win-icons converts the Linux tray PNGs into Windows .ico files.
//
// fyne.io/systray on Windows loads the tray image with LoadImageW(IMAGE_ICON)
// from a temp file, which parses the bytes as an .ico. PNG bytes don't work
// there, and PNG-compressed .ico entries aren't reliably accepted by
// LoadImage either — so we embed a classic 32-bit BGRA BMP (BITMAPINFOHEADER
// + bottom-up pixels + a zero AND mask), which every Windows version loads.
//
// Run once after changing the tray artwork: `go run ./cmd/gen-win-icons`.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

func main() {
	const srcDir = "cmd/murrly/assets/tray/linux"
	const dstDir = "cmd/murrly/assets/tray/windows"
	names := []string{"idle_44", "recording_44", "transcribing_44", "error_44"}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, name := range names {
		img := loadPNG(filepath.Join(srcDir, name+".png"))
		ico := pngToICO(img)
		out := filepath.Join(dstDir, name+".ico")
		if err := os.WriteFile(out, ico, 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s (%d bytes)", out, len(ico))
	}
}

func loadPNG(path string) *image.RGBA {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	return rgba
}

func pngToICO(img *image.RGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	// XOR bitmap: 32-bit BGRA, bottom-up.
	var xor bytes.Buffer
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			xor.WriteByte(byte(b >> 8))
			xor.WriteByte(byte(g >> 8))
			xor.WriteByte(byte(r >> 8))
			xor.WriteByte(byte(a >> 8))
		}
	}
	// AND mask: 1bpp, rows padded to 4 bytes, all zero (alpha carries opacity).
	andRow := ((w + 31) / 32) * 4
	andData := make([]byte, andRow*h)

	// BITMAPINFOHEADER (height doubled to cover XOR+AND, per ICO spec).
	var bih bytes.Buffer
	write := func(v any) { binary.Write(&bih, binary.LittleEndian, v) }
	write(uint32(40))     // biSize
	write(int32(w))       // biWidth
	write(int32(2 * h))   // biHeight
	write(uint16(1))      // biPlanes
	write(uint16(32))     // biBitCount
	write(uint32(0))      // biCompression = BI_RGB
	write(uint32(0))      // biSizeImage
	write(int32(0))       // biXPelsPerMeter
	write(int32(0))       // biYPelsPerMeter
	write(uint32(0))      // biClrUsed
	write(uint32(0))      // biClrImportant

	imageBlock := append(append(bih.Bytes(), xor.Bytes()...), andData...)

	// ICONDIR + one ICONDIRENTRY.
	var out bytes.Buffer
	w8 := func(v any) { binary.Write(&out, binary.LittleEndian, v) }
	w8(uint16(0))            // reserved
	w8(uint16(1))            // type = icon
	w8(uint16(1))            // count
	w8(byte(dim(w)))         // width (0 = 256)
	w8(byte(dim(h)))         // height
	w8(byte(0))              // color count
	w8(byte(0))              // reserved
	w8(uint16(1))            // planes
	w8(uint16(32))           // bit count
	w8(uint32(len(imageBlock)))
	w8(uint32(22)) // offset: 6 (ICONDIR) + 16 (one entry)
	out.Write(imageBlock)
	return out.Bytes()
}

// dim encodes an icon dimension: 256 is stored as 0 in the single-byte field.
func dim(n int) int {
	if n >= 256 {
		return 0
	}
	return n
}
