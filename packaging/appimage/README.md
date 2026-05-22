# Murrly AppImage build (Linux)

This directory will hold the AppImage build pipeline once it's wired up.
The plan, for when we get to it:

## Approach

1. Build a **CPU-only** Murrly binary (no CUDA dependency) — a CUDA-linked
   AppImage would only work on machines with matching CUDA runtime.
   CUDA users build from source as today.

2. Use `linuxdeploy` (https://github.com/linuxdeploy/linuxdeploy) to bundle
   the binary + dynamic deps (libportaudio, libX11, libxcb, libxkbfile,
   libgtk3 for systray) into an `AppDir/`.

3. Run `linuxdeploy --output appimage` to produce
   `Murrly-x86_64.AppImage` — a single executable file users download,
   `chmod +x`, and run.

## Required additions to the project

- A `mk/linux-cpu.mk` variant of the build (drops `-DGGML_CUDA=ON`,
  drops the CUDA linker flags).
- `make build-cpu` target in the root Makefile.
- `make appimage` target that:
  1. Calls `make build-cpu`
  2. Stages files into `build/AppDir/`
  3. Runs `linuxdeploy --output appimage`

## AppDir layout (to build by hand for now)

```
AppDir/
  AppRun                 # shell script: exec usr/bin/murrly "$@"
  murrly.desktop         # links Name, Exec=AppRun, Icon=murrly
  murrly.png             # 512×512 PNG from assets/icons/masters/
  usr/
    bin/murrly           # CPU-only binary
    lib/                 # bundled .so files (linuxdeploy fills in)
    share/applications/murrly.desktop
    share/icons/hicolor/512x512/apps/murrly.png
```

## Why not now

CUDA stripping needs a Linux build host. We can't test it from a macOS
dev machine. Defer until a Linux session with the toolchain installed.

In the meantime: Linux users build from source via
`scripts/bootstrap-ubuntu.sh` — the existing path works.
