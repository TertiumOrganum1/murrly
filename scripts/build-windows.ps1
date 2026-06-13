<#
build-windows.ps1 — build Murrly (murrly.exe + murrly-picker.exe) on Windows.

Toolchain (see README "Быстрый старт на Windows"):
  * MSYS2 MinGW-w64 gcc        — the cgo compiler (portaudio, whisper, Fyne).
  * Visual Studio 2019 + CUDA  — only for the CUDA whisper.cpp DLLs (nvcc needs cl).
  * The CMake + Ninja bundled with Visual Studio.

Two backends:
  -Backend cuda  (default) — whisper.cpp built as CUDA DLLs (MSVC/nvcc) and
                             linked from cgo via the MSVC import libs. Runs on
                             the GPU (RTX 4090). Ships the DLLs next to the exe.
  -Backend cpu             — whisper.cpp built as a MinGW static lib, no GPU.
                             Useful for a quick bring-up without the CUDA stack.

cgo links the whisper C API; the import libs / static libs only need the lib-
prefixed names MinGW's -l expects, which is what the staging step produces.
#>
[CmdletBinding()]
param(
    [ValidateSet('cuda', 'cpu')]
    [string]$Backend = 'cuda',
    [string]$MinGW = 'H:\msys64\mingw64',
    [string]$VSRoot = 'C:\Program Files (x86)\Microsoft Visual Studio\2019\Community',
    [string]$Cuda = 'C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.0',
    [switch]$SkipWhisper  # reuse an existing whisper build
)

$ErrorActionPreference = 'Stop'
$Repo = Split-Path -Parent $PSScriptRoot
$Whisper = Join-Path $Repo 'third_party\whisper.cpp'
$MinGWBin = Join-Path $MinGW 'bin'
# Forward-slash repo path for gcc/cgo -I/-L flags.
$RepoFwd = $Repo -replace '\\', '/'

if (-not (Test-Path (Join-Path $MinGWBin 'gcc.exe'))) {
    throw "MinGW gcc not found at $MinGWBin. Install MSYS2 and the mingw-w64 toolchain, or pass -MinGW."
}

function Build-WhisperCuda {
    Write-Host "==> Building whisper.cpp CUDA DLLs (MSVC + nvcc)..." -ForegroundColor Cyan
    & cmd.exe /c (Join-Path $PSScriptRoot 'build-whisper-cuda.bat')
    if ($LASTEXITCODE -ne 0) { throw "CUDA whisper build failed (exit $LASTEXITCODE)." }

    # MinGW's -lNAME wants lib<name>.a; the MSVC import libs (.lib) carry the C
    # API import descriptors that MinGW ld reads fine, so we just copy them
    # under the expected names. The DLLs keep their original names (their
    # inter-DLL imports reference those), so renaming only the import libs is
    # safe.
    $bcuda = Join-Path $Whisper 'build-win-cuda'
    $imp = Join-Path $bcuda 'mingw-implib'
    New-Item -ItemType Directory -Force -Path $imp | Out-Null
    Copy-Item -Force (Join-Path $bcuda 'src\whisper.lib')             (Join-Path $imp 'libwhisper.a')
    Copy-Item -Force (Join-Path $bcuda 'ggml\src\ggml.lib')          (Join-Path $imp 'libggml.a')
    Copy-Item -Force (Join-Path $bcuda 'ggml\src\ggml-base.lib')     (Join-Path $imp 'libggml-base.a')
    Copy-Item -Force (Join-Path $bcuda 'ggml\src\ggml-cpu.lib')      (Join-Path $imp 'libggml-cpu.a')
    return @{ ImpDir = ($imp -replace '\\', '/'); DllDir = (Join-Path $bcuda 'bin') }
}

function Build-WhisperCpu {
    Write-Host "==> Building whisper.cpp CPU static lib (MinGW)..." -ForegroundColor Cyan
    $cmake = Join-Path $VSRoot 'Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe'
    $ninja = Join-Path $VSRoot 'Common7\IDE\CommonExtensions\Microsoft\CMake\Ninja'
    $bcpu = Join-Path $Whisper 'build-win-cpu'
    $env:PATH = "$ninja;$MinGWBin;$env:PATH"
    # _WIN32_WINNT<0x0602 skips ggml's Win11 thread-throttling block, which
    # references SDK types missing from older MinGW headers. OpenMP off so we
    # don't have to ship/order libgomp.
    & $cmake -S $Whisper -B $bcpu -G Ninja -DCMAKE_C_COMPILER=gcc -DCMAKE_CXX_COMPILER=g++ `
        -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_CUDA=OFF -DGGML_OPENMP=OFF `
        -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF `
        -DCMAKE_C_FLAGS="-D_WIN32_WINNT=0x0601" -DCMAKE_CXX_FLAGS="-D_WIN32_WINNT=0x0601"
    if ($LASTEXITCODE -ne 0) { throw "CPU whisper configure failed." }
    & $cmake --build $bcpu --target whisper -j 8
    if ($LASTEXITCODE -ne 0) { throw "CPU whisper build failed." }

    # ggml static libs come out without the lib prefix (ggml.a) — copy to the
    # lib-prefixed names -lggml expects.
    $g = Join-Path $bcpu 'ggml\src'
    Copy-Item -Force (Join-Path $g 'ggml.a')      (Join-Path $g 'libggml.a')
    Copy-Item -Force (Join-Path $g 'ggml-base.a') (Join-Path $g 'libggml-base.a')
    Copy-Item -Force (Join-Path $g 'ggml-cpu.a')  (Join-Path $g 'libggml-cpu.a')
    return @{ ImpDir = "$RepoFwd/third_party/whisper.cpp/build-win-cpu/src;$RepoFwd/third_party/whisper.cpp/build-win-cpu/ggml/src"; DllDir = $null }
}

# --- build whisper.cpp -------------------------------------------------------
if (-not $SkipWhisper) {
    if ($Backend -eq 'cuda') { $w = Build-WhisperCuda } else { $w = Build-WhisperCpu }
} else {
    if ($Backend -eq 'cuda') {
        $w = @{ ImpDir = "$RepoFwd/third_party/whisper.cpp/build-win-cuda/mingw-implib"; DllDir = (Join-Path $Whisper 'build-win-cuda\bin') }
    } else {
        $w = @{ ImpDir = "$RepoFwd/third_party/whisper.cpp/build-win-cpu/src;$RepoFwd/third_party/whisper.cpp/build-win-cpu/ggml/src"; DllDir = $null }
    }
}

# --- go build ----------------------------------------------------------------
Write-Host "==> Building murrly.exe and murrly-picker.exe (cgo / MinGW)..." -ForegroundColor Cyan
$libFlags = ($w.ImpDir -split ';' | ForEach-Object { "-L$_" }) -join ' '
$env:PATH = "$MinGWBin;$env:PATH"   # gcc + pkg-config (portaudio-2.0.pc)
$env:CGO_ENABLED = '1'
$env:CC = 'gcc'
$env:CGO_CFLAGS = "-I$RepoFwd/third_party/whisper.cpp/include -I$RepoFwd/third_party/whisper.cpp/ggml/include"
$env:CGO_LDFLAGS = $libFlags

New-Item -ItemType Directory -Force -Path (Join-Path $Repo 'bin') | Out-Null
# -H=windowsgui builds for the GUI subsystem so launching the tray app (and the
# picker) doesn't pop a console window. stdin/stdout pipes still work for the
# picker because the parent inherits them regardless of subsystem.
& go build -ldflags "-H=windowsgui" -o (Join-Path $Repo 'bin\murrly.exe') ./cmd/murrly
if ($LASTEXITCODE -ne 0) { throw "go build murrly failed." }
# The picker is pure Go + Fyne (no whisper linkage); built without the cgo env above.
$env:CGO_CFLAGS = ''; $env:CGO_LDFLAGS = ''
& go build -ldflags "-H=windowsgui" -o (Join-Path $Repo 'bin\murrly-picker.exe') ./cmd/picker
if ($LASTEXITCODE -ne 0) { throw "go build picker failed." }

# --- stage runtime DLLs next to the exe --------------------------------------
Write-Host "==> Staging runtime DLLs into bin\ ..." -ForegroundColor Cyan
$bin = Join-Path $Repo 'bin'
# MinGW runtime (cgo) + portaudio — always needed.
foreach ($d in 'libgcc_s_seh-1.dll', 'libstdc++-6.dll', 'libwinpthread-1.dll', 'libportaudio.dll') {
    Copy-Item -Force (Join-Path $MinGWBin $d) $bin
}
if ($Backend -eq 'cuda') {
    Copy-Item -Force (Join-Path $w.DllDir '*.dll') $bin   # whisper/ggml/ggml-cuda
    foreach ($d in 'cudart64_12.dll', 'cublas64_12.dll', 'cublasLt64_12.dll') {
        Copy-Item -Force (Join-Path $Cuda "bin\$d") $bin
    }
    Write-Host "    (CUDA build also needs the VC++ 2015-2022 redistributable: msvcp140 / vcruntime140.)" -ForegroundColor DarkGray
}

Write-Host "`nDone. Binaries + DLLs are in $bin" -ForegroundColor Green
Write-Host "Run with: scripts\start-windows.ps1" -ForegroundColor Green
