#!/usr/bin/env bash
set -euo pipefail
WHISPER_DIR="${WHISPER_DIR:-third_party/whisper.cpp}"
if [[ ! -d "$WHISPER_DIR" ]]; then
    mkdir -p third_party
    git -C third_party clone --depth 1 https://github.com/ggml-org/whisper.cpp.git
fi

# Patch the Go binding so SetBeamSize is actually honoured. Upstream
# hardcodes whisper.SAMPLING_GREEDY in NewContext, which means strategy
# stays GREEDY no matter what beam_size we pass and beam_search.beam_size
# becomes dead state. Flipping to SAMPLING_BEAM_SEARCH makes width=1
# behave like greedy (whisper degrades cleanly at width 1) while letting
# width>1 actually trigger the beam_search decoder.
BIND_MODEL="$WHISPER_DIR/bindings/go/pkg/whisper/model.go"
if [[ -f "$BIND_MODEL" ]]; then
    sed -i.bak 's/whisper\.SAMPLING_GREEDY/whisper.SAMPLING_BEAM_SEARCH/g' "$BIND_MODEL"
    rm -f "$BIND_MODEL.bak"
fi

# Add the GPU/CPU-selectable loader to the binding. use_gpu is a model-load
# parameter and upstream's loader hardcodes the default (true), so without this
# there is no way to ask for a CPU context out of a CUDA build.
#
# These go in as whole new files rather than as a patch on purpose: the clone
# above is a shallow clone of upstream master, so the surroundings of any
# patched hunk drift with whatever upstream did that week, while a file that
# does not exist upstream has no context to drift against. Copying is also its
# own idempotency — running this script twice just overwrites with the same
# bytes, where `git apply` would fail the second time.
cp patches/whisper_gpu.go.in "$WHISPER_DIR/bindings/go/whisper_gpu.go"
cp patches/model_gpu.go.in "$WHISPER_DIR/bindings/go/pkg/whisper/model_gpu.go"
