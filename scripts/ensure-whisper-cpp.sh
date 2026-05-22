#!/usr/bin/env bash
set -euo pipefail
WHISPER_DIR="${WHISPER_DIR:-third_party/whisper.cpp}"
if [[ ! -d "$WHISPER_DIR" ]]; then
    mkdir -p third_party
    git -C third_party clone --depth 1 https://github.com/ggml-org/whisper.cpp.git
fi
