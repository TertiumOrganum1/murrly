//go:build darwin

package main

import (
	"log"
	"os"
	"path/filepath"
)

// setupMetalResources points whisper.cpp at the directory containing
// ggml-metal.metallib (the Metal shader library).
//
// In the typical macOS build, whisper.cpp is compiled with
// GGML_METAL_EMBED_LIBRARY=ON and shaders live inside libggml-metal.a,
// so no runtime setup is needed and this function is a no-op.
//
// If a future build disables embedding, the loose .metallib file would
// need to live next to the binary (dev workflow) or inside
// Murrly.app/Contents/Resources/ (production .app bundle). This helper
// looks for one in both locations and exposes it via the standard env
// var that the Metal backend consults at init time.
func setupMetalResources() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("metal: cannot resolve executable path: %v", err)
		return
	}
	exeDir := filepath.Dir(exe)

	for _, dir := range []string{
		filepath.Join(exeDir, "..", "Resources"),
		exeDir,
	} {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.metallib"))
		if len(matches) == 0 {
			continue
		}
		abs, _ := filepath.Abs(dir)
		if err := os.Setenv("GGML_METAL_PATH_RESOURCES", abs); err != nil {
			log.Printf("metal: setenv GGML_METAL_PATH_RESOURCES: %v", err)
			return
		}
		log.Printf("metal: GGML_METAL_PATH_RESOURCES=%s", abs)
		return
	}
	// No loose .metallib found — relying on shaders embedded in libggml-metal.a.
}
