# Windows build rules. The real work lives in the PowerShell scripts under
# scripts/ — the CUDA whisper.cpp build needs MSVC+nvcc (via a .bat that sets
# up the MSVC environment) and the cgo build needs the MinGW toolchain, which
# is awkward to drive from a Makefile. These targets just delegate so that
# `make build` / `make start` work for someone used to the Unix flow.
#
# Primary entry points on Windows are the scripts directly:
#   powershell -ExecutionPolicy Bypass -File scripts\bootstrap-windows.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\start-windows.ps1

PS := powershell.exe -ExecutionPolicy Bypass -NoProfile -File

whisper:
	cmd.exe /c scripts\\build-whisper-cuda.bat

build:
	$(PS) scripts/build-windows.ps1

start:
	$(PS) scripts/start-windows.ps1

model:
	@echo "On Windows, download models via: scripts\\bootstrap-windows.ps1 -Model <name>"

install:
	@echo "On Windows there is no system install step — run from bin\\ via scripts\\start-windows.ps1"
