# winuse installer for Windows: the binary and the agent skill, in one
# command.
#   irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
# Set WINUSE_NO_SKILL=1 to install only the binary.
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

# add to user PATH if missing, and to this session's PATH so winuse works
# without restarting the terminal (iex runs in the caller's session)
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
    Write-Host "added $dir to your user PATH"
}
if ($env:Path -notlike "*$dir*") {
    $env:Path = "$env:Path;$dir"
}
Write-Host "installed $exe"

# The agent skill, user-wide for every agent the skills CLI detects. It
# needs Node.js (npx); npx.cmd, not npx, so a restrictive execution policy
# cannot block npx.ps1. The CLI may report agents it cannot install to, so
# success is judged by the skill landing in ~\.agents\skills.
if ($env:WINUSE_NO_SKILL -eq "1") {
    Write-Host "skipped the agent skill (WINUSE_NO_SKILL=1); install it later with: npx skills add $repo -g"
} elseif (-not (Get-Command npx.cmd -ErrorAction SilentlyContinue)) {
    Write-Host "Node.js was not found, so the agent skill was not installed." -ForegroundColor Yellow
    Write-Host "install Node.js (https://nodejs.org), then run: npx skills add $repo -g"
} else {
    Write-Host "installing the agent skill: npx skills add $repo -g -y"
    $ErrorActionPreference = "Continue"
    & npx.cmd -y skills add $repo -g -y
    $ErrorActionPreference = "Stop"
    $skill = Join-Path $env:USERPROFILE ".agents\skills\winuse\SKILL.md"
    if (Test-Path $skill) {
        Write-Host "installed the winuse skill for your agents"
    } else {
        Write-Host "the agent skill may not have installed; run: npx skills add $repo -g" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "winuse is ready. Try: winuse list-windows"
Write-Host "or ask your agent: open Notepad and type hello world using winuse"
