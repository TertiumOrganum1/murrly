#!/usr/bin/env bash
set -euo pipefail

MODEL="${MODEL:-large-v3}"
INSTALL_SERVICE="${INSTALL_SERVICE:-0}"
INSTALL_CUDA_TOOLKIT="${INSTALL_CUDA_TOOLKIT:-0}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v apt-get >/dev/null 2>&1; then
	echo "This bootstrap script supports Debian/Ubuntu systems with apt-get." >&2
	exit 1
fi

if ! command -v sudo >/dev/null 2>&1; then
	echo "sudo is required to install system packages." >&2
	exit 1
fi

echo "Installing system packages..."
sudo apt-get update
sudo apt-get install -y \
	build-essential \
	ca-certificates \
	cmake \
	curl \
	git \
	libasound2-dev \
	libgtk-3-dev \
	libx11-dev \
	libxcursor-dev \
	libxi-dev \
	libxinerama-dev \
	libxkbfile-dev \
	libxrandr-dev \
	libxtst-dev \
	pkg-config \
	portaudio19-dev \
	xclip \
	xdotool

if ! sudo apt-get install -y libayatana-appindicator3-dev; then
	sudo apt-get install -y libappindicator3-dev
fi

if ! command -v go >/dev/null 2>&1; then
	sudo apt-get install -y golang-go
fi

if ! command -v nvcc >/dev/null 2>&1; then
	if [[ "$INSTALL_CUDA_TOOLKIT" == "1" ]]; then
		sudo apt-get install -y nvidia-cuda-toolkit
	else
		cat >&2 <<'MSG'
CUDA toolkit was not found (nvcc is missing).
Install NVIDIA CUDA Toolkit, or rerun with:

  INSTALL_CUDA_TOOLKIT=1 scripts/bootstrap-ubuntu.sh

MSG
		exit 1
	fi
fi

if [[ -z "${CUDA_HOST_COMPILER:-}" ]]; then
	if [[ -x /usr/bin/g++-8 ]]; then
		CUDA_HOST_COMPILER=/usr/bin/g++-8
	else
		CUDA_HOST_COMPILER="$(command -v g++ || true)"
	fi
fi

if [[ -z "$CUDA_HOST_COMPILER" ]]; then
	echo "No C++ compiler found. Install g++ or set CUDA_HOST_COMPILER=/path/to/g++." >&2
	exit 1
fi

echo "Using CUDA host compiler: $CUDA_HOST_COMPILER"
echo "Building whisper.cpp and downloading model: $MODEL"
make model MODEL="$MODEL" CUDA_HOST_COMPILER="$CUDA_HOST_COMPILER"

echo "Installing model into user data directory..."
mkdir -p "$HOME/.local/share/voice-input/models"
install -m 0644 "models/ggml-$MODEL.bin" "$HOME/.local/share/voice-input/models/"

echo "Building voice-input..."
make build CUDA_HOST_COMPILER="$CUDA_HOST_COMPILER"

if [[ "$INSTALL_SERVICE" == "1" ]]; then
	echo "Installing user service..."
	make install CUDA_HOST_COMPILER="$CUDA_HOST_COMPILER"
	systemctl --user enable --now voice-input.service
fi

cat <<MSG
Done.

Binary: $REPO_ROOT/bin/voice-input
Model:  $REPO_ROOT/models/ggml-$MODEL.bin

To install and start the user service later:
  INSTALL_SERVICE=1 scripts/bootstrap-ubuntu.sh
MSG
