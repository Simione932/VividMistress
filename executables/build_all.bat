@echo off
setlocal enabledelayedexpansion

:: ============================================================
:: PI-CHAT GATEWAY - Cross-Platform Build Script
:: ============================================================
:: Run this batch file from the project root (or double-click
:: it); it will compile for all supported platforms and place
:: the executables into organized folders under executables/
:: ============================================================

echo.
echo ============================================================
echo  PI-CHAT GATEWAY - Building for all platforms...
echo ============================================================
echo.

:: Get the directory where this batch file is located
set "SCRIPT_DIR=%~dp0"
:: Remove trailing backslash
set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

:: Change to the project root (parent of executables folder)
cd /d "%SCRIPT_DIR%\.."

echo Current directory: %cd%
echo.
echo ============================================================
echo.

:: Check if Go is installed
where go >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go is not installed or not in PATH.
    echo Please install Go from https://go.dev/dl/
    pause
    exit /b 1
)

echo Go version:
go version
echo.

:: Disable CGO for fully static cross-compilation
set CGO_ENABLED=0

:: Create all target directories
echo Creating target directories...
for %%d in (
    "freebsd_x64"
    "linux_arm64"
    "linux_arm_pi2"
    "linux_arm_pi3_32"
    "linux_arm_pi_zero_w"
    "linux_x64"
    "linux_x86"
    "mac_arm64"
    "mac_x64"
    "windows_arm64"
    "windows_x64"
    "windows_x86"
) do (
    if not exist "executables\%%~d" mkdir "executables\%%~d"
)
echo Done.
echo.

:: ============================================================
:: FreeBSD Target
:: ============================================================
echo [1/12] Building for FreeBSD x64...
set GOOS=freebsd
set GOARCH=amd64
set GOARM=
go build -ldflags="-s -w" -o executables\freebsd_x64\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

:: ============================================================
:: Linux Targets
:: ============================================================
echo [2/12] Building for Linux ARM64 / Raspberry Pi 3 64-bit...
set GOOS=linux
set GOARCH=arm64
set GOARM=
go build -ldflags="-s -w" -o executables\linux_arm64\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [3/12] Building for Raspberry Pi 2 (32-bit OS, ARMv7)...
set GOOS=linux
set GOARCH=arm
set GOARM=7
go build -ldflags="-s -w" -o executables\linux_arm_pi2\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [4/12] Building for Raspberry Pi 3 (32-bit OS, ARMv7)...
set GOOS=linux
set GOARCH=arm
set GOARM=7
go build -ldflags="-s -w" -o executables\linux_arm_pi3_32\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [5/12] Building for Raspberry Pi Zero W / Pi 1 (ARMv6)...
set GOOS=linux
set GOARCH=arm
set GOARM=6
go build -ldflags="-s -w" -o executables\linux_arm_pi_zero_w\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [6/12] Building for Linux x64...
set GOOS=linux
set GOARCH=amd64
set GOARM=
go build -ldflags="-s -w" -o executables\linux_x64\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [7/12] Building for Linux x86...
set GOOS=linux
set GOARCH=386
set GOARM=
go build -ldflags="-s -w" -o executables\linux_x86\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

:: ============================================================
:: macOS Targets
:: ============================================================
echo [8/12] Building for macOS Apple Silicon...
set GOOS=darwin
set GOARCH=arm64
set GOARM=
go build -ldflags="-s -w" -o executables\mac_arm64\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [9/12] Building for macOS Intel x64...
set GOOS=darwin
set GOARCH=amd64
set GOARM=
go build -ldflags="-s -w" -o executables\mac_x64\server .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

:: ============================================================
:: Windows Targets
:: ============================================================
echo [10/12] Building for Windows ARM64...
set GOOS=windows
set GOARCH=arm64
set GOARM=
go build -ldflags="-s -w" -o executables\windows_arm64\server.exe .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

echo [11/12] Building for Windows x64...
set GOOS=windows
set GOARCH=amd64
set GOARM=
go build -ldflags="-s -w" -o executables\windows_x64\server.exe .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

:: ============================================================
:: Windows Targets (cont.)
:: ============================================================
echo [12/12] Building for Windows x86...
set GOOS=windows
set GOARCH=386
set GOARM=
go build -ldflags="-s -w" -o executables\windows_x86\server.exe .\cmd\server
if %ERRORLEVEL% equ 0 (echo   OK) else (echo   FAILED)
echo.

:: ============================================================
:: Summary
:: ============================================================
echo ============================================================
echo  Build complete!
echo ============================================================
echo.
echo Output sizes:
echo.

for %%d in (
    "freebsd_x64\server"
    "linux_arm64\server"
    "linux_arm_pi2\server"
    "linux_arm_pi3_32\server"
    "linux_arm_pi_zero_w\server"
    "linux_x64\server"
    "mac_arm64\server"
    "mac_x64\server"
    "windows_arm64\server.exe"
    "windows_x64\server.exe"
    "windows_x86\server.exe"
) do (
    if exist "executables\%%~d" (
        for %%f in ("executables\%%~d") do (
            set "fname=%%~d"
            set "fsize=%%~zf"
            :: Pad the filename to align columns
            set "fname=!fname!                     "
            echo   !fname:~0,40! !fsize! bytes
        )
    ) else (
        set "fname=%%~d"
        set "fname=!fname!                     "
        echo   !fname:~0,40! [NOT BUILT]
    )
)

echo.
echo ============================================================
echo  All builds completed. Executables are in the executables/
echo  folder, organized by platform.
echo ============================================================
echo.

pause