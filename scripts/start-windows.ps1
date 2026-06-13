# start-windows.ps1 — launch the built Murrly in the background.
#
# Runs bin\murrly.exe detached so the console can close; Murrly keeps running
# from its tray icon. The required runtime DLLs (whisper/ggml/CUDA + the MinGW
# runtime + portaudio) are expected to sit next to murrly.exe — build-windows.ps1
# copies them there. Any already-running instance is replaced (murrly itself
# terminates stale instances at startup so two don't fight over the GPU).

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$exe  = Join-Path $root 'bin\murrly.exe'

if (-not (Test-Path $exe)) {
    Write-Error "murrly.exe not found at $exe — run scripts\build-windows.ps1 first."
    exit 1
}

Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe)
Write-Host "Murrly started — look for the cat icon in the system tray."
