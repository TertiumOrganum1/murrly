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
