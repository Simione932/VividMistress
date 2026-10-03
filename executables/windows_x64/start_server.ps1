# start_server.ps1 - Run the Vivid Mistress server in the background on Windows.
#
# Usage:
#   .\start_server.ps1                    Start the server in the background
#   .\start_server.ps1 -Background        Same as default; launch detached, no window
#   .\start_server.ps1 -Foreground        Run in the foreground (same window)
#   .\start_server.ps1 -Foreground -Log   Run in foreground, also write to server.log
#
# Extra arguments are passed to the server, e.g.:\
#   .\start_server.ps1 -addr :9090 -data C:\chatdata

param(
    [switch]$Background,
    [switch]$Foreground,
    [switch]$Log
)

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$Binary = Join-Path $ScriptDir "server.exe"
$LogFile = Join-Path $ScriptDir "server.log"

if (-not (Test-Path $Binary)) {
    Write-Host "Error: '$Binary' not found."
    Write-Host "Put the 'server.exe' binary in this folder first."
    exit 1
}

if ($Foreground) {
    if ($Log) {
        Write-Host "Starting server in foreground, logging to $LogFile ..."
        & $Binary @args 2>&1 | Tee-Object -FilePath $LogFile -Append
    } else {
        Write-Host "Starting server in foreground... press Ctrl+C to stop."
        & $Binary @args
    }
} else {
    Write-Host "Starting server in the background..."
    Write-Host "  Log: $LogFile"
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Binary
    $psi.Arguments = ($args -join " ")
    $psi.WorkingDirectory = $ScriptDir
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.WindowStyle = [System.Diagnostics.ProcessWindowStyle]::Hidden
    $psi.CreateNoWindow = $true
    $proc = [System.Diagnostics.Process]::Start($psi)
    Start-Job -InputObject $proc -ScriptBlock {
        param($p, $logFile)
        $p.StandardOutput.BeginReadLine()
        $p.StandardError.BeginReadLine()
        while (-not $p.HasExited) {
            $line = $p.StandardOutput.ReadLine()
            if ($line) { Add-Content -Path $logFile -Value $line }
            $line = $p.StandardError.ReadLine()
            if ($line) { Add-Content -Path $logFile -Value $line }
            Start-Sleep -Milliseconds 100
        }
    } -ArgumentList $proc, $LogFile | Out-Null
    Write-Host "Server started in the background."
    Write-Host "  Foreground? Run: .\start_server.ps1 -Foreground"
    Write-Host "  Stop: taskkill /IM server.exe /F"
}
