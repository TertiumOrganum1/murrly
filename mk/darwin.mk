WHISPER_CMAKE_FLAGS := -DCMAKE_BUILD_TYPE=Release \
                       -DBUILD_SHARED_LIBS=OFF \
                       -DGGML_METAL=ON \
                       -DGGML_ACCELERATE=ON
CGO_LDFLAGS_DARWIN := -framework Foundation -framework Metal \
                      -framework MetalKit -framework Accelerate \
                      -framework CoreFoundation -framework ApplicationServices
INSTALL_DATA_DIR := $(HOME)/Library/Application Support/Murrly

$(WHISPER_BUILD)/src/libwhisper.a:
	@scripts/ensure-whisper-cpp.sh
	cmake -S $(WHISPER_DIR) -B $(WHISPER_BUILD) $(WHISPER_CMAKE_FLAGS)
	cmake --build $(WHISPER_BUILD) --target whisper -j$$(sysctl -n hw.ncpu)

build: whisper
	mkdir -p bin
	CGO_LDFLAGS="$(CGO_LDFLAGS_DARWIN)" go build -o $(BIN) ./cmd/murrly

install: build
	scripts/install-mac.sh

start:
	open -a "Murrly" || ./$(BIN)

autostart: build
	scripts/install-mac.sh --autostart

uninstall-autostart:
	osascript -e 'tell application "System Events" to delete login item "Murrly"' || true
