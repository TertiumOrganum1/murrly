INCLUDE_PATH := $(abspath $(WHISPER_DIR)/include):$(abspath $(WHISPER_DIR)/ggml/include)
LIBRARY_PATH := $(abspath $(WHISPER_BUILD)/src):$(abspath $(WHISPER_BUILD)/ggml/src):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cpu):$(abspath $(WHISPER_BUILD)/ggml/src/ggml-cuda):/usr/lib/x86_64-linux-gnu
# -rpath-link lets the linker resolve TRANSITIVE shared deps (libX11 → libxcb,
# libportaudio → libjack) from the system lib dir. Needed when a non-system ld
# is first in PATH (e.g. a linuxbrew binutils), which otherwise doesn't search
# /usr/lib/x86_64-linux-gnu for indirect deps and fails with undefined
# xcb_*/jack_* references. Harmless with the system ld.
CUDA_LDFLAGS = -Wl,-rpath-link=/usr/lib/x86_64-linux-gnu -Wl,--build-id=0x$(CUDA_BUILD_ID) -lggml-cuda -lcudart -lcublas -lcuda -lstdc++
CUDA_HOST_COMPILER ?= /usr/bin/g++-8

# GGML_CUDA_LIB / CUDA_BUILD_ID close a trap that cost a whole debugging
# session. go build caches the external link step, and its cache key covers
# the ldflags STRING but not the CONTENTS of the static libraries those flags
# name. Rebuild libggml-cuda.a with different CUDA kernels and go happily
# hands back the previously linked binary — same size, same bytes, new mtime.
# The binary that shipped that way carried sm_75 cubins for an hour after the
# library on disk had been rebuilt as PTX, and every measurement taken against
# it was measuring the old kernels. `go clean -cache` did not dislodge it.
#
# So the archive's own digest goes into --build-id: a real, meaningful linker
# flag (it identifies the build) that also makes the ldflags string change
# whenever the kernels change, which is exactly what the cache key needs.
GGML_CUDA_LIB := $(WHISPER_BUILD)/ggml/src/ggml-cuda/libggml-cuda.a
CUDA_BUILD_ID = $(shell md5sum $(GGML_CUDA_LIB) 2>/dev/null | cut -c1-32)
INSTALL_DATA_DIR := $(HOME)/.local/share/murrly

# CUDA_ARCHS pins the architectures ggml builds kernels for, and on this pair
# of cards the choice is not the obvious one.
#
# The GTX 1660 is Turing (sm_75) but belongs to the TU116 line, which ships
# WITHOUT tensor cores. Building 75-real gives it a cubin full of MMQ kernels
# written around the `mma` instruction those cores provide — the card takes
# the slow path through every one of them. Measured with whisper-bench: 9666 ms
# to encode a single 30 s chunk, against ~1 s of what the card is worth.
#
# So no 75 here on purpose. 61-virtual is Pascal PTX, which the driver JITs for
# sm_75 and which routes the same MMQ kernels through dp4a instead — the
# arrangement whisper.cpp itself prints at load time on any GTX 16xx. 80-virtual
# covers Ampere and, by JIT, the Ada card. Both are PTX, so the first load after
# a rebuild spends a while in the driver's compiler; the result is cached in
# ~/.nv/ComputeCache and later loads are immediate.
#
# FORCE_MMQ is the other half of that advice: use the integer matrix-multiply
# kernels for quantized weights rather than the tensor-core path.
CUDA_ARCHS ?= 61-virtual;80-virtual

$(WHISPER_BUILD)/src/libwhisper.a:
	@scripts/ensure-whisper-cpp.sh
	cmake -S $(WHISPER_DIR) -B $(WHISPER_BUILD) \
		-DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF \
		-DGGML_CUDA=ON \
		-DGGML_CUDA_FORCE_MMQ=ON \
		-DCMAKE_CUDA_ARCHITECTURES="$(CUDA_ARCHS)" \
		-DCMAKE_CUDA_HOST_COMPILER=$(CUDA_HOST_COMPILER)
	cmake --build $(WHISPER_BUILD) --target whisper -j$$(nproc)

# whisper-rebuild forces the configure step to run again. The library target
# above is a file rule, so it never re-runs once libwhisper.a exists — which
# means a changed CUDA_ARCHS or FORCE_MMQ would otherwise be silently ignored.
.PHONY: whisper-rebuild
whisper-rebuild:
	cmake -S $(WHISPER_DIR) -B $(WHISPER_BUILD) \
		-DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF \
		-DGGML_CUDA=ON \
		-DGGML_CUDA_FORCE_MMQ=ON \
		-DCMAKE_CUDA_ARCHITECTURES="$(CUDA_ARCHS)" \
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
