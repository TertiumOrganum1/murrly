SHELL := /bin/bash
WHISPER_DIR := third_party/whisper.cpp
WHISPER_BUILD := $(WHISPER_DIR)/build
BIN := bin/voice-input

INCLUDE_PATH := $(abspath $(WHISPER_DIR)/include):$(abspath $(WHISPER_DIR)/ggml/include)
LIBRARY_PATH := $(abspath $(WHISPER_BUILD)/src):$(abspath $(WHISPER_BUILD)/ggml/src):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cpu):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cuda):/usr/lib/x86_64-linux-gnu

CUDA_LDFLAGS := -lggml-cuda -lcudart -lcublas -lcuda -lstdc++
CUDA_HOST_COMPILER ?= /usr/bin/g++-8
MODEL ?= large-v3
MODEL_DIR := models
INSTALL_DATA_DIR := $(HOME)/.local/share/voice-input

.PHONY: all whisper model build install autostart uninstall-autostart clean

all: build

whisper: $(WHISPER_BUILD)/src/libwhisper.a

$(WHISPER_BUILD)/src/libwhisper.a:
	@if [ ! -d $(WHISPER_DIR) ]; then \
		mkdir -p third_party && \
		git -C third_party clone https://github.com/ggml-org/whisper.cpp.git; \
	fi
	cmake -S $(WHISPER_DIR) -B $(WHISPER_BUILD) \
		-DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF \
		-DGGML_CUDA=ON \
		-DCMAKE_CUDA_HOST_COMPILER=$(CUDA_HOST_COMPILER)
	cmake --build $(WHISPER_BUILD) --target whisper -j$$(nproc)

model: whisper
	mkdir -p $(MODEL_DIR)
	$(WHISPER_DIR)/models/download-ggml-model.sh $(MODEL) $(MODEL_DIR)

build: whisper
	mkdir -p bin
	C_INCLUDE_PATH="$(INCLUDE_PATH)" \
	LIBRARY_PATH="$(LIBRARY_PATH)" \
	go build -ldflags "-extldflags '$(CUDA_LDFLAGS)'" -o $(BIN) ./cmd/voice-input

install: build
	INSTALL_DATA_DIR="$(INSTALL_DATA_DIR)" scripts/install-linux-desktop.sh

autostart: build
	INSTALL_DATA_DIR="$(INSTALL_DATA_DIR)" AUTOSTART=1 scripts/install-linux-desktop.sh

uninstall-autostart:
	rm -f $$HOME/.config/autostart/voice-input.desktop

clean:
	rm -rf bin
	rm -rf $(WHISPER_BUILD)
