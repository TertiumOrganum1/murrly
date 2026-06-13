<#
bootstrap-windows.ps1 — one-shot setup + build of Murrly on Windows (CUDA).

Steps:
  1. Sanity-check the toolchain (MSYS2 MinGW gcc, Visual Studio, CUDA, Go).
  2. Install the MSYS2 MinGW packages cgo needs (pkgconf + portaudio).
  3. Download a Whisper model into %LocalAppData%\Murrly\models\.
  4. Build whisper.cpp (CUDA) + murrly.exe + murrly-picker.exe and stage DLLs.

Prerequisites you must install yourself first (all free):
  * MSYS2            https://www.msys2.org/         (gives MinGW-w64 gcc)
  * Go 1.25+         https://go.dev/dl/
  * Visual Studio 2019/2022 with "Desktop development with C++" (MSVC + the
    bundled CMake/Ninja) — needed only to compile the CUDA whisper DLLs.
  * CUDA Toolkit 12.x https://developer.nvidia.com/cuda-downloads
  * NVIDIA driver for your GPU.

Usage:   powershell -ExecutionPolicy Bypass -File scripts\bootstrap-windows.ps1
Options: -Model large-v3-turbo   (default; or large-v3 / large-v3-turbo-q5_0 / tiny)
         -Backend cpu            (skip CUDA, build a CPU-only whisper)
#>
[CmdletBinding()]
param(
    [string]$Model = 'large-v3-turbo',
    [ValidateSet('cuda', 'cpu')]
    [string]$Backend = 'cuda',
    [string]$Msys2 = ''  # MSYS2 root; auto-detected if empty
)

$ErrorActionPreference = 'Stop'
$Repo = Split-Path -Parent $PSScriptRoot

# Find-Msys2 returns the MSYS2 root: the -Msys2 override, then the usual install
# locations, then derived from gcc already on PATH.
function Find-Msys2 {
    if ($Msys2) { return $Msys2 }
    $roots = @($env:MSYS2_ROOT, 'C:\msys64', "$env:SystemDrive\msys64", 'H:\msys64', 'D:\msys64')
    foreach ($r in $roots) {
        if ($r -and (Test-Path (Join-Path $r 'mingw64\bin\gcc.exe'))) { return $r }
    }
    $g = Get-Command gcc.exe -ErrorAction SilentlyContinue
    if ($g) { return (Split-Path (Split-Path (Split-Path $g.Source))) } # ...\mingw64\bin\gcc.exe -> root
    throw "MSYS2 not found. Install it (https://www.msys2.org/) or pass -Msys2 <root>."
}
$Msys2 = Find-Msys2

function Need($name, $cmd) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Warning "$name not found on PATH ($cmd). See the header of this script."
    } else {
        Write-Host ("  ok: {0}" -f $name) -ForegroundColor DarkGreen
    }
}

Write-Host "==> Checking toolchain..." -ForegroundColor Cyan
Need 'Go' 'go'
$mingwGcc = Join-Path $Msys2 'mingw64\bin\gcc.exe'
if (Test-Path $mingwGcc) { Write-Host "  ok: MinGW gcc ($mingwGcc)" -ForegroundColor DarkGreen }
else { Write-Warning "MinGW gcc not found at $mingwGcc — install MSYS2 or pass -Msys2." }

# --- 1. MSYS2 packages (pkgconf + portaudio) ---------------------------------
Write-Host "==> Installing MSYS2 MinGW packages (pkgconf, portaudio)..." -ForegroundColor Cyan
$shell = Join-Path $Msys2 'msys2_shell.cmd'
if (Test-Path $shell) {
    & cmd.exe /c "`"$shell`" -mingw64 -defterm -no-start -c `"pacman -S --noconfirm --needed mingw-w64-x86_64-pkgconf mingw-w64-x86_64-portaudio`""
} else {
    Write-Warning "msys2_shell.cmd not found at $shell — install pkgconf + portaudio manually via pacman."
}

# --- 2. model ----------------------------------------------------------------
$modelsDir = Join-Path $env:LOCALAPPDATA 'Murrly\models'
New-Item -ItemType Directory -Force -Path $modelsDir | Out-Null
$modelFile = Join-Path $modelsDir "ggml-$Model.bin"
if (Test-Path $modelFile) {
    Write-Host "==> Model already present: $modelFile" -ForegroundColor DarkGreen
} else {
    $url = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$Model.bin"
    Write-Host "==> Downloading model $Model ..." -ForegroundColor Cyan
    Write-Host "    $url"
    curl.exe -L -o $modelFile $url
}

# --- 3. build ----------------------------------------------------------------
Write-Host "==> Building Murrly ($Backend)..." -ForegroundColor Cyan
& (Join-Path $PSScriptRoot 'build-windows.ps1') -Backend $Backend -MinGW (Join-Path $Msys2 'mingw64')

Write-Host "`nBootstrap complete." -ForegroundColor Green
Write-Host "Start Murrly:  powershell -File scripts\start-windows.ps1" -ForegroundColor Green
Write-Host "Hold F12 to dictate; Ctrl+F11 for the variants window." -ForegroundColor Green
