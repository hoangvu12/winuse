# winuse installer for Windows.
#   irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
$ErrorActionPreference = "Stop"

$repo = "hoangvu12/winuse"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$asset = "winuse_windows_$arch.exe"
$url = "https://github.com/$repo/releases/latest/download/$asset"

$dir = Join-Path $env:LOCALAPPDATA "winuse"
New-Item -ItemType Directory -Force $dir | Out-Null
$exe = Join-Path $dir "winuse.exe"

# Download to a temporary file first: a failed or partial download must
# never replace a working winuse.exe, and a running helper locks the old
# binary's file (Windows refuses to overwrite a running executable).
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "winuse-install-$([System.IO.Path]::GetRandomFileName()).exe"

Write-Host "downloading $url"
try {
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
    } catch {
        Write-Host "error: the download failed: $($_.Exception.Message)" -ForegroundColor Red
        if (Test-Path $exe) {
            Write-Host "the existing install at $exe was left unchanged"
        } else {
            Write-Host "no winuse is installed yet; if no release exists, build from source instead:"
            Write-Host "  git clone https://github.com/$repo"
            Write-Host '  cd winuse; $env:CGO_ENABLED=0; go build -o winuse.exe ./cmd/winuse'
        }
        exit 1
    }

    # Stop a running winuse helper so the old binary can be replaced: a
    # Windows process locks its own executable file. The next winuse
    # command starts a fresh helper automatically.
    $running = Get-Process -Name winuse -ErrorAction SilentlyContinue
    if ($running) {
        Write-Host "stopping the running winuse helper (the next winuse command restarts it)"
        $running | Stop-Process -Force
        # give the OS a moment to release the file locks
        Start-Sleep -Milliseconds 500
    }

    try {
        Move-Item -Force -Path $tmp -Destination $exe
    } catch {
        Write-Host "error: replacing $exe failed: $($_.Exception.Message)" -ForegroundColor Red
        Write-Host "a winuse process may still hold the file; close it and rerun the installer"
        exit 1
    }
} finally {
    if (Test-Path $tmp) {
        Remove-Item -Force $tmp -ErrorAction SilentlyContinue
    }
}

# The v1 binary is unsigned. Strip the Mark of the Web so SmartScreen does
# not flag the first run on the agent's behalf; if Defender still flags it,
# the README's "Allowing the unsigned binary" section is the walkthrough.
Unblock-File -Path $exe -ErrorAction SilentlyContinue

# add to user PATH if missing
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
    Write-Host "added $dir to your user PATH (restart your terminal)"
}

Write-Host "installed $exe"
Write-Host "next: winuse list-windows - or install the skill: npx skills add $repo"
