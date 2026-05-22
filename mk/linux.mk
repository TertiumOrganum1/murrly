INCLUDE_PATH := $(abspath $(WHISPER_DIR)/include):$(abspath $(WHISPER_DIR)/ggml/include)
LIBRARY_PATH := $(abspath $(WHISPER_BUILD)/src):$(abspath $(WHISPER_BUILD)/ggml/src):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cpu):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cuda):/usr/lib/x86_64-linux-gnu
CUDA_LDFLAGS := -lggml-cuda -lcudart -lcublas -lcuda -lstdc++
CUDA_HOST_COMPILER ?= /usr/bin/g++-8
INSTALL_DATA_DIR := $(HOME)/.local/share/murrly

$(WHISPER_BUILD)/src/libwhisper.a:
	@scripts/ensure-whisper-cpp.sh
	cmake -S $(WHISPER_DIR) -B $(WHISPER_BUILD) \
		-DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF \
		-DGGML_CUDA=ON \
		-DCMAKE_CUDA_HOST_COMPILER=$(CUDA_HOST_COMPILER)
	cmake --build $(WHISPER_BUILD) --target whisper -j$$(nproc)

build: whisper
	mkdir -p bin
	C_INCLUDE_PATH="$(INCLUDE_PATH)" \
	LIBRARY_PATH="$(LIBRARY_PATH)" \
	go build -ldflags "-extldflags '$(CUDA_LDFLAGS)'" -o $(BIN) ./cmd/murrly

install: build
	INSTALL_DATA_DIR="$(INSTALL_DATA_DIR)" scripts/install-linux.sh

start:
	scripts/start-linux.sh

autostart: build
	INSTALL_DATA_DIR="$(INSTALL_DATA_DIR)" AUTOSTART=1 scripts/install-linux.sh

uninstall-autostart:
	rm -f $$HOME/.config/autostart/murrly.desktop
