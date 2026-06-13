@echo off
setlocal

REM Build whisper.cpp as CUDA shared DLLs (MSVC + nvcc) for linking from the
REM MinGW cgo build. Auto-detects Visual Studio (vswhere) and CUDA (CUDA_PATH).
REM Override the target GPU arch with CUDA_ARCH (default 89 = Ada / RTX 40xx;
REM e.g. set CUDA_ARCH=86 for RTX 30xx, 75 for RTX 20xx).

REM Start from a clean Windows PATH. Launched from a POSIX shell (Git Bash) the
REM inherited PATH is colon-separated and confuses cmd's program lookup, which
REM breaks vcvars/vswhere and nvcc's bare cl.exe.
set "PATH=C:\Windows\System32;C:\Windows;C:\Windows\System32\Wbem;C:\Windows\System32\WindowsPowerShell\v1.0"

REM --- locate Visual Studio (any 2019/2022, Community/Pro/BuildTools) ---
set "VSWHERE=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSWHERE%" set "VSWHERE=%ProgramFiles%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSWHERE%" ( echo ERROR: vswhere not found; install Visual Studio with the C++ workload & exit /b 1 )
set "VSROOT="
for /f "usebackq tokens=*" %%i in (`"%VSWHERE%" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set "VSROOT=%%i"
if not defined VSROOT ( echo ERROR: no Visual Studio with C++ tools found & exit /b 1 )
set "CMAKE=%VSROOT%\Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin\cmake.exe"
set "NINJADIR=%VSROOT%\Common7\IDE\CommonExtensions\Microsoft\CMake\Ninja"

REM --- locate CUDA ---
if not defined CUDA_PATH ( echo ERROR: CUDA_PATH not set; install the CUDA Toolkit & exit /b 1 )
set "CUDA=%CUDA_PATH%"
if not defined CUDA_ARCH set "CUDA_ARCH=89"

call "%VSROOT%\VC\Auxiliary\Build\vcvars64.bat" || exit /b 1
set "PATH=%CUDA%\bin;%NINJADIR%;%PATH%"

cd /d "%~dp0.."

"%CMAKE%" -S third_party\whisper.cpp -B third_party\whisper.cpp\build-win-cuda -G Ninja ^
  -DCMAKE_BUILD_TYPE=Release ^
  -DBUILD_SHARED_LIBS=ON ^
  -DGGML_CUDA=ON ^
  -DGGML_OPENMP=OFF ^
  -DWHISPER_BUILD_TESTS=OFF ^
  -DWHISPER_BUILD_EXAMPLES=OFF ^
  -DCMAKE_CUDA_ARCHITECTURES=%CUDA_ARCH% || exit /b 1

"%CMAKE%" --build third_party\whisper.cpp\build-win-cuda --target whisper -j 8 || exit /b 1

echo CUDA_WHISPER_BUILD_DONE
