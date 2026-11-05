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

# The agent skill, user-wide. With Node.js, the skills CLI links it into
# every agent it detects (npx.cmd, not npx, so a restrictive execution
# policy cannot block npx.ps1). Either way the binary then writes its own
# embedded copy (SKILL.md and its topics) to the user-level folders the
# harnesses read, so the skill always matches the installed winuse.exe and
# the skill step works without Node.js:
#   ~\.agents\skills  Codex, Copilot, Cursor, Gemini CLI and most others,
#                     and the folder the skills CLI links from
#   ~\.claude\skills  Claude Code, which never reads ~\.agents\skills
#                     (CLAUDE_CONFIG_DIR\skills when that is set)
$claudeHome = if ($env:CLAUDE_CONFIG_DIR) { $env:CLAUDE_CONFIG_DIR } else { Join-Path $env:USERPROFILE ".claude" }
$skillDirs = @(
    (Join-Path $env:USERPROFILE ".agents\skills"),
    (Join-Path $claudeHome "skills")
)
if ($env:WINUSE_NO_SKILL -eq "1") {
    Write-Host "skipped the agent skill (WINUSE_NO_SKILL=1); install it later with:"
    foreach ($dir in $skillDirs) { Write-Host "  winuse skill install `"$dir`"" }
} else {
    if (Get-Command npx.cmd -ErrorAction SilentlyContinue) {
        Write-Host "linking the agent skill into your agents: npx skills add $repo -g -y"
        $ErrorActionPreference = "Continue"
        try {
            & npx.cmd -y skills add $repo -g -y
        } catch {
            Write-Host "npx skills add failed: $($_.Exception.Message)" -ForegroundColor Yellow
        }
        $ErrorActionPreference = "Stop"
    } else {
        Write-Host "Node.js was not found, so the skill goes only to $($skillDirs -join ' and ')." -ForegroundColor Yellow
        Write-Host "for agents that read another skills folder, install Node.js (https://nodejs.org) and run: npx skills add $repo -g"
    }
    # Each folder on its own: one that fails (a permission, a broken link)
    # is reported and the other is still written. A folder whose winuse is
    # a link to the other's (npx skills' layout) is written through the
    # link, which is fine: it is the same folder again.
    $installedIn = @()
    foreach ($dir in $skillDirs) {
        $ErrorActionPreference = "Continue"
        try {
            $output = & $exe skill install $dir 2>&1 | ForEach-Object { "$_" }
            $ok = $LASTEXITCODE -eq 0
        } catch {
            $output = $_.Exception.Message
            $ok = $false
        }
        $ErrorActionPreference = "Stop"
        if ($ok) {
            $installedIn += $dir
        } else {
            Write-Host "the agent skill did not install in $dir ($($output -join ' ')); run: winuse skill install `"$dir`"" -ForegroundColor Yellow
        }
    }
    if ($installedIn.Count -gt 0) {
        Write-Host "installed the winuse skill for your agents in $($installedIn -join ' and ') (winuse skill prints it)"
    }
}

Write-Host ""
Write-Host "winuse is ready. Try: winuse list-windows"
Write-Host "or ask your agent: open Notepad and type hello world using winuse"
