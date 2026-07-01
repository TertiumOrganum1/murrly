INCLUDE_PATH := $(abspath $(WHISPER_DIR)/include):$(abspath $(WHISPER_DIR)/ggml/include)
LIBRARY_PATH := $(abspath $(WHISPER_BUILD)/src):$(abspath $(WHISPER_BUILD)/ggml/src):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cpu):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cuda):/usr/lib/x86_64-linux-gnu
# -rpath-link lets the linker resolve TRANSITIVE shared deps (libX11 → libxcb,
# libportaudio → libjack) from the system lib dir. Needed when a non-system ld
# is first in PATH (e.g. a linuxbrew binutils), which otherwise doesn't search
# /usr/lib/x86_64-linux-gnu for indirect deps and fails with undefined
# xcb_*/jack_* references. Harmless with the system ld.
CUDA_LDFLAGS := -Wl,-rpath-link=/usr/lib/x86_64-linux-gnu -lggml-cuda -lcudart -lcublas -lcuda -lstdc++
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
	# The multi-inference picker is a standalone Fyne GUI (separate binary
	# because systray and a Fyne window can't share the main OS thread).
	# Pure Go + OpenGL — no whisper/CUDA linkage, so no special env needed.
	go build -o bin/picker ./cmd/picker

# test runs the full Go test suite with the same linker paths as build,
# so the cgo-using packages (transcriber, anything else hitting
# libwhisper / libggml) can resolve their externals. Without these env
# vars `go test ./...` from a bare shell fails on internal/transcriber
# even though the code is fine — the linker just can't find the
# vendored whisper.cpp static libs.
test: whisper
	C_INCLUDE_PATH="$(INCLUDE_PATH)" \
	LIBRARY_PATH="$(LIBRARY_PATH)" \
	go test -ldflags "-extldflags '$(CUDA_LDFLAGS)'" ./...

install: build
	INSTALL_DATA_DIR="$(INSTALL_DATA_DIR)" scripts/install-linux.sh

start:
	scripts/start-linux.sh

# stop / restart — single-command wrappers around the kill+install+start
# cycle so a tooling-agnostic caller (CI, editor task runner, an
# automation permissions allow-list) can redeploy Murrly without composing
# the steps by hand. restart is the common "I changed code, deploy
# it" target: kill the running binary, rebuild via install's deps,
# launch the fresh one.
stop:
	@-pkill -x murrly 2>/dev/null || true

restart:
	@-pkill -x murrly 2>/dev/null || true
	@$(MAKE) install
	@scripts/start-linux.sh

autostart: build
	INSTALL_DATA_DIR="$(INSTALL_DATA_DIR)" AUTOSTART=1 scripts/install-linux.sh

uninstall-autostart:
	rm -f $$HOME/.config/autostart/murrly.desktop
