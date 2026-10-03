# ============================================================
# PI-CHAT GATEWAY - Cross-Platform PowerShell Build Script
# ============================================================

Write-Host "`n============================================================"
Write-Host " PI-CHAT GATEWAY - Building for all platforms..."
Write-Host "============================================================`n"

# Set working directory to project root (parent of executables folder)
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location (Join-Path $ScriptDir "..")

Write-Host "Current directory: $(Get-Location)`n"
Write-Host "============================================================`n"

# Check if Go is installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[ERROR] Go is not installed or not in PATH." -ForegroundColor Red
    Write-Host "Please install Go from https://go.dev/dl/"
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "Go version:"
go version
Write-Host ""

# Disable CGO for fully static cross-compilation
$env:CGO_ENABLED = "0"

# Targets configuration
$targets = @(
    @{ Name = "[1/12] Building for FreeBSD x64..."; OS = "freebsd"; Arch = "amd64"; Arm = ""; Dir = "freebsd_x64"; Out = "server" },
    @{ Name = "[2/12] Building for Linux ARM64 / Raspberry Pi 3 64-bit..."; OS = "linux"; Arch = "arm64"; Arm = ""; Dir = "linux_arm64"; Out = "server" },
    @{ Name = "[3/12] Building for Raspberry Pi 2 (32-bit OS, ARMv7)..."; OS = "linux"; Arch = "arm"; Arm = "7"; Dir = "linux_arm_pi2"; Out = "server" },
    @{ Name = "[4/12] Building for Raspberry Pi 3 (32-bit OS, ARMv7)..."; OS = "linux"; Arch = "arm"; Arm = "7"; Dir = "linux_arm_pi3_32"; Out = "server" },
    @{ Name = "[5/12] Building for Raspberry Pi Zero W / Pi 1 (ARMv6)..."; OS = "linux"; Arch = "arm"; Arm = "6"; Dir = "linux_arm_pi_zero_w"; Out = "server" },
    @{ Name = "[6/12] Building for Linux x64..."; OS = "linux"; Arch = "amd64"; Arm = ""; Dir = "linux_x64"; Out = "server" },
    @{ Name = "[7/12] Building for Linux x86..."; OS = "linux"; Arch = "386"; Arm = ""; Dir = "linux_x86"; Out = "server" },
    @{ Name = "[8/12] Building for macOS Apple Silicon..."; OS = "darwin"; Arch = "arm64"; Arm = ""; Dir = "mac_arm64"; Out = "server" },
    @{ Name = "[9/12] Building for macOS Intel x64..."; OS = "darwin"; Arch = "amd64"; Arm = ""; Dir = "mac_x64"; Out = "server" },
    @{ Name = "[10/12] Building for Windows ARM64..."; OS = "windows"; Arch = "arm64"; Arm = ""; Dir = "windows_arm64"; Out = "server.exe" },
    @{ Name = "[11/12] Building for Windows x64..."; OS = "windows"; Arch = "amd64"; Arm = ""; Dir = "windows_x64"; Out = "server.exe" },
    @{ Name = "[12/12] Building for Windows x86..."; OS = "windows"; Arch = "386"; Arm = ""; Dir = "windows_x86"; Out = "server.exe" }
)

# Ensure target directories exist
Write-Host "Creating target directories..."
foreach ($target in $targets) {
    $dirPath = Join-Path "executables" $target.Dir
    if (-not (Test-Path $dirPath)) {
        New-Item -ItemType Directory -Path $dirPath | Out-Null
    }
}
Write-Host "Done.`n"

# Execute builds
foreach ($target in $targets) {
    Write-Host $target.Name

    $env:GOOS = $target.OS
    $env:GOARCH = $target.Arch
    $env:GOARM = $target.Arm

    $outputPath = Join-Path "executables" (Join-Path $target.Dir $target.Out)

    go build -ldflags="-s -w" -o $outputPath ./cmd/server

    if ($LASTEXITCODE -eq 0) {
        Write-Host "  OK" -ForegroundColor Green
    } else {
        Write-Host "  FAILED" -ForegroundColor Red
    }
    Write-Host ""
}

# Clean environment variables
$env:GOOS = ""
$env:GOARCH = ""
$env:GOARM = ""

# Summary output
Write-Host "============================================================"
Write-Host " Build complete!"
Write-Host "============================================================`n"
Write-Host "Output sizes:`n"

foreach ($target in $targets) {
    $relPath = Join-Path $target.Dir $target.Out
    $fullPath = Join-Path "executables" $relPath

    if (Test-Path $fullPath) {
        $file = Get-Item $fullPath
        "{0,-45} {1:N0} bytes" -f $relPath, $file.Length
    } else {
        "{0,-45} [NOT BUILT]" -f $relPath
    }
}

Write-Host "`n============================================================"
Write-Host " All builds completed. Executables are in executables/"
Write-Host "============================================================`n"