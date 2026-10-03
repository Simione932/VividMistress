@echo off
REM start_server.bat - Run the Vivid Mistress server on Windows.
REM
REM Usage:
REM   start_server.bat            Start the server (foreground in this console)
REM   start_server.bat -log       Start the server, writing output to server.log
REM
REM Extra arguments are passed to the server, e.g.:\
REM   start_server.bat -addr :9090 -data C:\chatdata
REM
REM The server runs in the current console window. To run it truly in the
REM background (no console window at all), use the companion PowerShell script:
REM   powershell -ExecutionPolicy Bypass -File start_server.ps1 -Background

setlocal

set "DIR=%~dp0"
set "BIN=%DIR%server.exe"
set "LOG_FILE=%DIR%server.log"

REM Check for -log flag
set "LOG_IT="
if /I "%~1"=="-log" (
    set "LOG_IT=1"
    shift
)

REM Verify the executable exists
if not exist "%BIN%" (
    echo Error: '%BIN%' not found.
    echo Put the 'server.exe' binary in this folder first.
    exit /b 1
)

if defined LOG_IT (
    echo Starting server... output directed to %LOG_FILE%
    "%BIN%" %* >> "%LOG_FILE%" 2>&1
) else (
    echo Starting server... press Ctrl+C to stop.
    "%BIN%" %*
)

endlocal
