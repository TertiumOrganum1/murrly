@echo off
REM Build whisper.cpp as CUDA shared DLLs (MSVC + nvcc), for linking from the
REM MinGW cgo build of Murrly. Produces whisper.dll + ggml*.dll + ggml-cuda.dll
REM and MSVC import .lib files under third_party\whisper.cpp\build-win-cuda.
REM
REM Requires: Visual Studio 2019 (or Build Tools) with MSVC, CUDA Toolkit 12.x,
REM and the CMake + Ninja bundled with Visual Studio. Run from a normal cmd;
REM it sets up the MSVC environment itself.

setlocal

REM Start from a clean Windows PATH. When this .bat is launched from a POSIX
REM shell (Git Bash / MSYS), the inherited PATH is colon-separated and confuses
REM cmd's program lookup — vcvars then can't find vswhere.exe and nvcc's bare
REM "cl.exe" invocations fail. Seeding the essential Windows dirs (+ the VS
REM Installer that hosts vswhere) lets vcvars set up the full MSVC/CUDA env.
set "PATH=C:\Windows\System32;C:\Windows;C:\Windows\System32\Wbem;C:\Windows\System32\WindowsPowerShell\v1.0;C:\Program Files (x86)\Microsoft Visual Studio\Installer"

set "VSROOT=C:\Program Files (x86)\Microsoft Visual Studio\2019\Community"
set "CUDA=C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.0"
set "CMAKE=%VSROOT%\Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe"
set "NINJADIR=%VSROOT%\Common7\IDE\CommonExtensions\Microsoft\CMake\Ninja"

call "%VSROOT%\VC\Auxiliary\Build\vcvars64.bat" || exit /b 1
set "PATH=%CUDA%\bin;%NINJADIR%;%PATH%"

cd /d "%~dp0.."

REM CMAKE_CUDA_ARCHITECTURES=89 targets the RTX 4090 (Ada / sm_89).
"%CMAKE%" -S third_party\whisper.cpp -B third_party\whisper.cpp\build-win-cuda -G Ninja ^
  -DCMAKE_BUILD_TYPE=Release ^
  -DBUILD_SHARED_LIBS=ON ^
  -DGGML_CUDA=ON ^
  -DGGML_OPENMP=OFF ^
  -DWHISPER_BUILD_TESTS=OFF ^
  -DWHISPER_BUILD_EXAMPLES=OFF ^
  -DCMAKE_CUDA_ARCHITECTURES=89 || exit /b 1

"%CMAKE%" --build third_party\whisper.cpp\build-win-cuda --target whisper -j 8 || exit /b 1

echo CUDA_WHISPER_BUILD_DONE
