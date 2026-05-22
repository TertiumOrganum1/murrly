BREW_PREFIX := $(shell brew --prefix 2>/dev/null || echo /opt/homebrew)

WHISPER_CMAKE_FLAGS := -DCMAKE_BUILD_TYPE=Release \
                       -DBUILD_SHARED_LIBS=OFF \
                       -DGGML_METAL=ON \
                       -DGGML_METAL_EMBED_LIBRARY=ON \
                       -DGGML_ACCELERATE=ON

# Search paths picked up by clang/cgo.
INCLUDE_PATH := $(abspath $(WHISPER_DIR)/include):$(abspath $(WHISPER_DIR)/ggml/include):$(BREW_PREFIX)/include
LIBRARY_PATH := $(abspath $(WHISPER_BUILD)/src):$(abspath $(WHISPER_BUILD)/ggml/src):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cpu):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-metal):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-blas):$(BREW_PREFIX)/lib

# pkg-config lookup for portaudio (the gordonklaus/portaudio binding uses
# `#cgo pkg-config: portaudio-2.0`). Brew's .pc lives under $(BREW_PREFIX).
PKG_CONFIG_PATH := $(BREW_PREFIX)/lib/pkgconfig:$(BREW_PREFIX)/share/pkgconfig

# Linker flags: whisper static libs in dependency order, system libs, frameworks.
CGO_LDFLAGS_DARWIN := -lwhisper -lggml -lggml-cpu -lggml-base -lggml-metal -lggml-blas -lc++ \
                      -framework Foundation -framework Metal -framework MetalKit \
                      -framework Accelerate -framework CoreFoundation \
                      -framework ApplicationServices

INSTALL_DATA_DIR := $(HOME)/Library/Application Support/Murrly

$(WHISPER_BUILD)/src/libwhisper.a:
	@scripts/ensure-whisper-cpp.sh
	cmake -S $(WHISPER_DIR) -B $(WHISPER_BUILD) $(WHISPER_CMAKE_FLAGS)
	cmake --build $(WHISPER_BUILD) --target whisper -j$$(sysctl -n hw.ncpu)
	cmake --build $(WHISPER_BUILD) --target ggml -j$$(sysctl -n hw.ncpu)
	cmake --build $(WHISPER_BUILD) --target ggml-cpu -j$$(sysctl -n hw.ncpu)
	cmake --build $(WHISPER_BUILD) --target ggml-base -j$$(sysctl -n hw.ncpu)
	cmake --build $(WHISPER_BUILD) --target ggml-metal -j$$(sysctl -n hw.ncpu)
	cmake --build $(WHISPER_BUILD) --target ggml-blas -j$$(sysctl -n hw.ncpu)

# Sources that should trigger a rebuild. Go's cgo only tracks .go file
# mtimes — it ignores .m/.h/.png changes, so we list them explicitly and
# force `go build` whenever any of them is newer than the binary.
GO_SOURCES   := $(shell find cmd internal -type f -name '*.go' 2>/dev/null)
CGO_SOURCES  := $(shell find cmd internal -type f \( -name '*.m' -o -name '*.h' -o -name '*.c' \) 2>/dev/null)
ASSET_SOURCES := $(shell find cmd/murrly/assets -type f 2>/dev/null)
ALL_SOURCES  := $(GO_SOURCES) $(CGO_SOURCES) $(ASSET_SOURCES)

build: $(BIN)

$(BIN): $(WHISPER_BUILD)/src/libwhisper.a $(ALL_SOURCES)
	mkdir -p bin
	# -a forces re-link so .m/.h changes always picked up, since go-build's
	# default dependency graph misses non-.go files in the same package.
	C_INCLUDE_PATH="$(INCLUDE_PATH)" \
	LIBRARY_PATH="$(LIBRARY_PATH)" \
	PKG_CONFIG_PATH="$(PKG_CONFIG_PATH)" \
	CGO_LDFLAGS="$(CGO_LDFLAGS_DARWIN)" \
	go build -trimpath -buildvcs=false -a -o $(BIN) ./cmd/murrly

install: build
	scripts/install-mac.sh

start:
	open -a "Murrly" || ./$(BIN)

autostart: build
	scripts/install-mac.sh --autostart

uninstall-autostart:
	osascript -e 'tell application "System Events" to delete login item "Murrly"' || true
