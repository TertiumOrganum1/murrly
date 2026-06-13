<#
build-windows.ps1 — build Murrly (murrly.exe + murrly-picker.exe) on Windows.

Toolchain (auto-detected; override with the params below):
  * MSYS2 MinGW-w64 gcc        — the cgo compiler (portaudio, whisper, Fyne).
  * Visual Studio + CUDA       — only for the CUDA whisper.cpp DLLs (nvcc needs cl).
  * The CMake + Ninja bundled with Visual Studio.

Two backends:
  -Backend cuda  (default) — whisper.cpp built as CUDA DLLs (MSVC/nvcc) and
                             linked from cgo via the MSVC import libs; runs on
                             the GPU. The DLLs are staged next to the exe.
  -Backend cpu             — whisper.cpp built as a MinGW static lib, no GPU.

Overrides: -MinGW <root>\mingw64, -VSRoot <vs install path>, -Cuda <cuda path>.
#>
[CmdletBinding()]
param(
    [ValidateSet('cuda', 'cpu')]
    [string]$Backend = 'cuda',
    [string]$MinGW = '',            # MSYS2 mingw64 dir; auto-detected if empty
    [string]$VSRoot = '',           # VS install path; auto-detected via vswhere if empty
    [string]$Cuda = $env:CUDA_PATH, # CUDA toolkit dir; from %CUDA_PATH% by default
    [switch]$SkipWhisper            # reuse an existing whisper build
)

$ErrorActionPreference = 'Stop'
$Repo = Split-Path -Parent $PSScriptRoot
$Whisper = Join-Path $Repo 'third_party\whisper.cpp'
$RepoFwd = $Repo -replace '\\', '/'

# Find-MinGW returns the MSYS2 mingw64 directory: the -MinGW override, then the
# usual install locations, then gcc already on PATH.
function Find-MinGW {
    if ($MinGW) { return $MinGW }
    $roots = @($env:MSYS2_ROOT, 'C:\msys64', "$env:SystemDrive\msys64", 'H:\msys64', 'D:\msys64')
    foreach ($r in $roots) {
        if ($r -and (Test-Path (Join-Path $r 'mingw64\bin\gcc.exe'))) { return (Join-Path $r 'mingw64') }
    }
    $g = Get-Command gcc.exe -ErrorAction SilentlyContinue
    if ($g) { return (Split-Path (Split-Path $g.Source)) } # ...\mingw64\bin\gcc.exe -> ...\mingw64
    throw "MSYS2 MinGW not found. Install MSYS2 (https://www.msys2.org/) or pass -MinGW <root>\mingw64."
}

# Find-VSRoot locates any Visual Studio / Build Tools install with the C++
# workload via vswhere (works for 2019/2022, Community/Pro/BuildTools).
function Find-VSRoot {
    if ($VSRoot) { return $VSRoot }
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
    if (-not (Test-Path $vswhere)) { throw "vswhere not found — install Visual Studio (or Build Tools) with the 'Desktop development with C++' workload." }
    $p = (& $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath) | Select-Object -First 1
    if (-not $p) { throw "No Visual Studio with C++ tools found (vswhere returned nothing)." }
    return $p.Trim()
}

$MinGW = Find-MinGW
$MinGWBin = Join-Path $MinGW 'bin'
Write-Host "MinGW: $MinGW" -ForegroundColor DarkGray

function Build-WhisperCuda {
    Write-Host "==> Building whisper.cpp CUDA DLLs (MSVC + nvcc)..." -ForegroundColor Cyan
    & cmd.exe /c (Join-Path $PSScriptRoot 'build-whisper-cuda.bat')
    if ($LASTEXITCODE -ne 0) { throw "CUDA whisper build failed (exit $LASTEXITCODE)." }

    # MinGW's -lNAME wants lib<name>.a; the MSVC import libs (.lib) carry the C
    # API import descriptors MinGW ld reads fine, so we copy them under the
    # expected names. The DLLs keep their original names (inter-DLL imports
    # reference those), so renaming only the import libs is safe.
    $bcuda = Join-Path $Whisper 'build-win-cuda'
    $imp = Join-Path $bcuda 'mingw-implib'
    New-Item -ItemType Directory -Force -Path $imp | Out-Null
    Copy-Item -Force (Join-Path $bcuda 'src\whisper.lib')         (Join-Path $imp 'libwhisper.a')
    Copy-Item -Force (Join-Path $bcuda 'ggml\src\ggml.lib')       (Join-Path $imp 'libggml.a')
    Copy-Item -Force (Join-Path $bcuda 'ggml\src\ggml-base.lib')  (Join-Path $imp 'libggml-base.a')
    Copy-Item -Force (Join-Path $bcuda 'ggml\src\ggml-cpu.lib')   (Join-Path $imp 'libggml-cpu.a')
    return @{ ImpDir = ($imp -replace '\\', '/'); DllDir = (Join-Path $bcuda 'bin') }
}

function Build-WhisperCpu {
    Write-Host "==> Building whisper.cpp CPU static lib (MinGW)..." -ForegroundColor Cyan
    $vs = Find-VSRoot
    $cmake = Join-Path $vs 'Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe'
    $ninja = Join-Path $vs 'Common7\IDE\CommonExtensions\Microsoft\CMake\Ninja'
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
    if (-not $Cuda) { throw "CUDA path unknown (set %CUDA_PATH% or pass -Cuda) — can't stage the CUDA runtime DLLs." }
    # version-agnostic: cudart64_12.dll, cublas64_12.dll, ... whatever 12.x ships.
    foreach ($pat in 'cudart64_*.dll', 'cublas64_*.dll', 'cublasLt64_*.dll') {
        Get-ChildItem (Join-Path $Cuda 'bin') -Filter $pat -ErrorAction SilentlyContinue |
            ForEach-Object { Copy-Item -Force $_.FullName $bin }
    }
    Write-Host "    (CUDA build also needs the VC++ 2015-2022 redistributable: msvcp140 / vcruntime140.)" -ForegroundColor DarkGray
}

Write-Host "`nDone. Binaries + DLLs are in $bin" -ForegroundColor Green
Write-Host "Run with: scripts\start-windows.ps1" -ForegroundColor Green
