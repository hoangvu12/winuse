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

Write-Host "downloading $url"
try {
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $exe
} catch {
    Write-Host "error: download failed. If no release exists yet, build from source:" -ForegroundColor Red
    Write-Host "  git clone https://github.com/$repo; cd winuse; CGO_ENABLED=0 go build -o winuse.exe ./cmd/winuse"
    exit 1
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
