<#
.SYNOPSIS
Records the README demo: three named Agents at once, each in its own
browser window, each with its own Agent Cursor and name tag.

.DESCRIPTION
1. Builds winuse from this repo to %TEMP%\winuse-demo\winuse.exe (or uses
   -Exe), stops the running helper and starts the demo build's helper with
   WINUSE_CURSOR_CAPTURE=1, so the screen recorder sees the Agent Cursors.
2. Starts Chrome on winuse's own launch profile (the profile a
   `winuse browser launch chrome` uses, so winuse finds its Browser
   Connection) with four chromeless --app windows on the demo pages in
   .\site: a dark backdrop, and over it, tiled in a fixed rectangle,
   "Pixel studio", "Launch board" and "Release notes". Nothing else is
   recorded: only the rectangle, which the backdrop fills.
3. Three Agents read their window (get-window-state), then act at once,
   each in a background job with its own WINUSE_AGENT and
   WINUSE_CURSOR_COLOR. Page Actions in different browser windows take
   different lanes, so the cursors move together:
     artist  (pink)   colours a rocket in Pixel studio: 12 clicks
     planner (violet) sets the board's owner, ticks 5 tasks, ships v2.0
     writer  (green)  types the release notes, sets the channel, publishes
4. ffmpeg records the rectangle (ddagrab by default, gdigrab with
   -Grabber gdigrab) at 30 fps from just before the Agents start until
   3 s after the last one ends.
5. Closes the demo windows (only the Chrome processes on that profile),
   restarts the helper normally (capture exclusion back on), and encodes
   docs\images\winuse-demo.gif (palettegen/paletteuse, about 960 px wide,
   under 8 MB) and out\winuse-demo.mp4.

Every winuse command's exit code is checked; any failure stops the run,
and the cleanup still closes the demo windows and restores the helper.

It never touches windows it did not open, never saves anything, and never
types into the user's apps.

.PARAMETER Exe
A winuse.exe to use instead of building one. It must have
WINUSE_CURSOR_CAPTURE support (the readme-demo branch or later).

.PARAMETER Grabber
ddagrab (default; Desktop Duplication, physical pixels) or gdigrab.

.PARAMETER SelfTest
Do nothing on the desktop: encode a synthetic clip the size of the
rectangle through the same GIF/MP4 steps into %TEMP%\winuse-demo\selftest.

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File .scratch\readme-demo\record-demo.ps1
#>
[CmdletBinding()]
param(
    [string]$Exe = "",
    [ValidateSet("ddagrab", "gdigrab")][string]$Grabber = "ddagrab",
    [switch]$SelfTest
)

Set-StrictMode -Version 3
$ErrorActionPreference = "Stop"

$Repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Site = Join-Path $PSScriptRoot "site"
$Out = Join-Path $PSScriptRoot "out"
$Work = Join-Path $env:TEMP "winuse-demo"
$Gif = Join-Path $Repo "docs\images\winuse-demo.gif"
$Mp4 = Join-Path $Out "winuse-demo.mp4"
$Raw = Join-Path $Work "raw.mkv"
$Installed = Join-Path $env:LOCALAPPDATA "winuse\winuse.exe"
$HelperLog = Join-Path $env:LOCALAPPDATA "winuse\helper.log"
$DemoProfile = Join-Path $env:LOCALAPPDATA "winuse\browser\profile-chrome"
$Chrome = Join-Path $env:ProgramFiles "Google\Chrome\Application\chrome.exe"

# The recorded rectangle (physical pixels), centred across the primary
# display and kept clear of the taskbar. Each of the three windows is
# (2400 - 4 x 30) / 3 = 760 px wide: 506 DIP at 150 %, past Chrome's
# 500 DIP minimum window width. The GIF is 960 px wide.
$RecW = 2400
$RecH = 1300
$GifWidth = 960
$GifMaxBytes = 8MB

function Say([string]$m) { Write-Host ("[{0:HH:mm:ss}] {1}" -f (Get-Date), $m) }

function Fail([string]$m) { throw "record-demo: $m" }

# --- encoding (also run by -SelfTest) -------------------------------------

function Invoke-Ffmpeg([string[]]$FfArgs) {
    & ffmpeg -hide_banner -loglevel error -y @FfArgs
    if ($LASTEXITCODE -ne 0) { Fail "ffmpeg failed (exit $LASTEXITCODE): ffmpeg $($FfArgs -join ' ')" }
}

function Get-Duration([string]$Path) {
    $d = & ffprobe -v error -show_entries format=duration -of default=nw=1:nk=1 $Path
    if ($LASTEXITCODE -ne 0) { Fail "ffprobe could not read $Path" }
    return [double]::Parse(($d | Select-Object -First 1), [Globalization.CultureInfo]::InvariantCulture)
}

function Get-Size([string]$Path) {
    $s = & ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=s=x:p=0 $Path
    if ($LASTEXITCODE -ne 0) { Fail "ffprobe could not read $Path" }
    return ($s | Select-Object -First 1).Trim()
}

# Encode-Demo turns the raw recording into the README GIF and an MP4,
# skipping the first $Lead seconds (ffmpeg warming up, the go). The GIF gets one
# palette for the whole clip (stats_mode=diff favours what moves) and is
# re-encoded smaller until it fits $GifMaxBytes.
function Encode-Demo([string]$Source, [string]$GifPath, [string]$Mp4Path, [double]$Lead) {
    $dur = Get-Duration $Source
    if ($dur - $Lead -lt 5) { Fail ("the recording is {0:N1} s long: too short" -f $dur) }
    New-Item -ItemType Directory -Force (Split-Path $GifPath) | Out-Null
    New-Item -ItemType Directory -Force (Split-Path $Mp4Path) | Out-Null
    $ss = $Lead.ToString([Globalization.CultureInfo]::InvariantCulture)

    Say "encoding $Mp4Path"
    Invoke-Ffmpeg @("-ss", $ss, "-i", $Source, "-vf", "scale=1600:-2:flags=lanczos", "-c:v", "libx264", "-preset", "slow",
        "-crf", "22", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-an", $Mp4Path)

    foreach ($try in @(@{ fps = 15; w = $GifWidth; colors = 192 }, @{ fps = 12; w = 880; colors = 160 }, @{ fps = 10; w = 800; colors = 128 })) {
        Say ("encoding {0} at {1} fps, {2} px wide" -f $GifPath, $try.fps, $try.w)
        $vf = "fps=$($try.fps),scale=$($try.w):-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=$($try.colors):stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle"
        Invoke-Ffmpeg @("-ss", $ss, "-i", $Source, "-filter_complex", $vf, "-loop", "0", $GifPath)
        $bytes = (Get-Item $GifPath).Length
        Say ("GIF: {0:N1} MB, {1:N1} s, {2}" -f ($bytes / 1MB), (Get-Duration $GifPath), (Get-Size $GifPath))
        if ($bytes -le $GifMaxBytes) { return }
    }
    Fail ("the GIF is still over {0} MB at 10 fps, 800 px" -f ($GifMaxBytes / 1MB))
}

if ($SelfTest) {
    $st = Join-Path $Work "selftest"
    New-Item -ItemType Directory -Force $st | Out-Null
    $src = Join-Path $st "raw.mkv"
    Say "self-test: a synthetic ${RecW}x${RecH} clip, 20 s at 30 fps"
    Invoke-Ffmpeg @("-f", "lavfi", "-i", "testsrc2=size=${RecW}x${RecH}:rate=30:duration=20", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "12", $src)
    Encode-Demo $src (Join-Path $st "winuse-demo.gif") (Join-Path $st "winuse-demo.mp4") 1.0
    Say "self-test done: $st"
    return
}

# --- winuse ----------------------------------------------------------------

function Invoke-Winuse([string[]]$WArgs) {
    $out = & $script:Exe @WArgs 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { Fail "winuse $($WArgs -join ' ') failed (exit $LASTEXITCODE):`n$out" }
    return $out
}

# Get-HelperPid is the running helper's pid from `winuse status` (which
# never starts one), or 0.
function Get-HelperPid {
    $s = & $script:Exe status 2>&1 | Out-String
    if ($s -match "helper: running, [^,]+, pid (\d+)") { return [int]$Matches[1] }
    return 0
}

# Stop-Helper ends every running winuse helper (there is no stop command;
# a helper holds no state an Agent needs between commands) and waits for
# its pipe to go.
function Stop-Helper {
    $procs = @(Get-CimInstance Win32_Process -Filter "Name='winuse.exe'" | Where-Object { $_.CommandLine -match "__helper" })
    foreach ($p in $procs) {
        Say "stopping the winuse helper (pid $($p.ProcessId), $($p.ExecutablePath))"
        Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
    }
    for ($i = 0; $i -lt 50 -and (Get-HelperPid) -ne 0; $i++) { Start-Sleep -Milliseconds 100 }
    if ((Get-HelperPid) -ne 0) { Fail "the old helper is still running" }
}

# Start-CaptureHelper restarts the helper from the demo build with
# WINUSE_CURSOR_CAPTURE=1 and checks it took it: its log says so after
# its own start line.
function Start-CaptureHelper {
    Stop-Helper
    $env:WINUSE_CURSOR_CAPTURE = "1"
    Invoke-Winuse @("list-windows") | Out-Null
    $hp = Get-HelperPid
    if ($hp -eq 0) { Fail "no helper started" }
    $log = Get-Content $HelperLog -Tail 400
    $start = -1
    for ($i = 0; $i -lt $log.Count; $i++) { if ($log[$i] -match "starting \(pid $hp\)") { $start = $i } }
    if ($start -lt 0) { Fail "the helper log has no start line for pid $hp" }
    $on = $false
    for ($i = $start; $i -lt $log.Count; $i++) { if ($log[$i] -match "WINUSE_CURSOR_CAPTURE=1") { $on = $true } }
    if (-not $on) { Fail "helper $hp did not take WINUSE_CURSOR_CAPTURE=1 (is $script:Exe the readme-demo build?)" }
    Say "helper $hp records its Agent Cursors"
    return $hp
}

# Restore-Helper stops the demo helper and starts the installed winuse's
# helper with no WINUSE_CURSOR_CAPTURE, so the cursors are excluded from
# capture again.
function Restore-Helper {
    Remove-Item Env:WINUSE_CURSOR_CAPTURE -ErrorAction SilentlyContinue
    Stop-Helper
    $exe = $Installed
    if (-not (Test-Path $exe)) { $exe = $script:Exe }
    $o = & $exe list-windows 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { Write-Warning "restarting the helper normally failed: $o"; return }
    Say "helper restarted normally from $exe (cursors excluded from capture again)"
}

# --- the demo windows ------------------------------------------------------

function Get-FreePort {
    $l = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $l.Start()
    $p = $l.LocalEndpoint.Port
    $l.Stop()
    return $p
}

# Get-DemoChromes is always an array: call it as @(Get-DemoChromes), since
# PowerShell returns an empty one as $null.
function Get-DemoChromes {
    $needle = $DemoProfile.ToLowerInvariant()
    return @(Get-CimInstance Win32_Process -Filter "Name='chrome.exe'" |
            Where-Object { $_.CommandLine -and $_.CommandLine.ToLowerInvariant().Contains($needle) })
}

function Start-DemoWindow([string]$Page, [int]$Port, [string[]]$Extra) {
    $url = ([Uri](Join-Path $Site $Page)).AbsoluteUri
    $a = @("--remote-debugging-port=$Port", "--user-data-dir=`"$DemoProfile`"", "--no-first-run", "--no-default-browser-check",
        "--disable-features=CalculateNativeWinOcclusion,msImplicitSignin", "--disable-session-crashed-bubble",
        "--hide-crash-restore-bubble", "--disable-search-engine-choice-screen", "--app=$url") + $Extra
    Start-Process -FilePath $Chrome -ArgumentList $a | Out-Null
}

# Wait-DemoWindow waits for the window titled $Title and checks it is ours:
# a Chrome window of a process on the demo profile. It returns its ID.
function Wait-DemoWindow([string]$Title) {
    $line = Invoke-Winuse @("wait", "window", $Title, "--timeout", "30")
    $id = ($line -split "`t")[0].Trim()
    if ($id -notmatch '^\d+$') { Fail "could not read the window ID of '$Title' from: $line" }
    $details = Invoke-Winuse @("list-windows", "--details")
    $row = $details -split "`r?`n" | Where-Object { $_ -match "^$id`t" } | Select-Object -First 1
    if (-not $row -or $row -notmatch "pid (\d+)") { Fail "window $id ('$Title') is not listed: $details" }
    $winPid = [int]$Matches[1]
    if (@(Get-DemoChromes | Where-Object { $_.ProcessId -eq $winPid }).Count -eq 0) {
        Fail "window $id ('$Title', pid $winPid) is not a Chrome on the demo profile: refusing to act on it"
    }
    return $id
}

function Place-Window([string]$Id, [int]$X, [int]$Y, [int]$W, [int]$H) {
    Invoke-Winuse @("move-window", $Id, "$X", "$Y") | Out-Null
    Invoke-Winuse @("resize-window", $Id, "$W", "$H") | Out-Null
}

function Close-DemoWindows([string[]]$Ids) {
    foreach ($id in $Ids) {
        if ($id) { & $script:Exe close-window $id 2>&1 | Out-Null }
    }
    for ($i = 0; $i -lt 50 -and @(Get-DemoChromes).Count -gt 0; $i++) { Start-Sleep -Milliseconds 100 }
    foreach ($p in @(Get-DemoChromes)) { Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue }
}

# --- the Agents ------------------------------------------------------------

# Each step is a winuse command with the window left out: it goes right
# after the command name.
$Agents = @(
    @{
        name = "artist"; color = "#C73782"; title = "Pixel studio"; pause = 0
        steps = @(
            @("click", "name:Silver"), @("click", "name:Pixel 6-5"),
            @("click", "name:Sky"), @("click", "name:Pixel 8-7"),
            @("click", "name:Red"), @("click", "name:Pixel 4-7"), @("click", "name:Pixel 12-3"), @("click", "name:Pixel 12-12"),
            @("click", "name:Orange"), @("click", "name:Pixel 15-7"),
            @("click", "name:Midnight"), @("click", "name:Pixel 1-1")
        )
    },
    @{
        name = "planner"; color = "#6B58CE"; title = "Launch board"; pause = 900
        steps = @(
            @("set-value", "name:Board owner", "Agents"),
            @("click", "name:Done: Write changelog"), @("click", "name:Done: Record demo"), @("click", "name:Done: Update docs"),
            @("click", "name:Done: Tag the release"), @("click", "name:Done: Announce it"),
            @("click", "name:Ship v2.0")
        )
    },
    @{
        name = "writer"; color = "#1F853E"; title = "Release notes"; pause = 1100
        steps = @(
            @("type-text", "--into", "name:Release title", "winuse 2.0"),
            @("set-value", "name:Release channel", "Stable"),
            @("type-text", "--into", "name:First highlight", "Many agents, one desktop"),
            @("type-text", "--into", "name:Second highlight", "Each with its own cursor"),
            @("type-text", "--into", "name:Third highlight", "Your mouse stays yours"),
            @("click", "name:Publish notes")
        )
    }
)

# The job each Agent runs: read its window, say it is ready, wait for the
# go, then run its steps, each checked.
$AgentJob = {
    param($Exe, $Window, $Name, $Color, $StepsJson, $PauseMs, $Ready, $Go, $Log)
    $ErrorActionPreference = "Stop"
    $env:WINUSE_AGENT = $Name
    $env:WINUSE_CURSOR_COLOR = $Color
    $run = {
        param([string[]]$A)
        $o = & $Exe @A 2>&1 | Out-String
        Add-Content -Path $Log -Value ("> winuse " + ($A -join " ") + "`n" + $o)
        if ($LASTEXITCODE -ne 0) { throw "$Name`: winuse $($A -join ' ') failed (exit $LASTEXITCODE): $o" }
    }
    & $run -A @("get-window-state", $Window)
    Set-Content -Path $Ready -Value "ready"
    $deadline = (Get-Date).AddSeconds(120)
    while (-not (Test-Path $Go)) {
        if ((Get-Date) -gt $deadline) { throw "$Name`: no go within 120 s" }
        Start-Sleep -Milliseconds 20
    }
    $first = $true
    foreach ($s in (ConvertFrom-Json $StepsJson)) {
        if (-not $first -and $PauseMs -gt 0) { Start-Sleep -Milliseconds $PauseMs }
        $first = $false
        $s = @($s)
        $a = @($s[0], $Window)
        if ($s.Count -gt 1) { $a += $s[1..($s.Count - 1)] }
        & $run -A ([string[]]$a)
    }
}

# --- the run ---------------------------------------------------------------

foreach ($tool in @("ffmpeg", "ffprobe")) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Fail "$tool is not on PATH" }
}
if (-not (Test-Path $Chrome)) { Fail "Chrome is not at $Chrome" }
if (@(Get-DemoChromes).Count -gt 0) { Fail "a Chrome is already running on $DemoProfile; close it first" }
New-Item -ItemType Directory -Force $Work | Out-Null
Get-ChildItem $Work -Filter "*.flag" -ErrorAction SilentlyContinue | Remove-Item
Get-ChildItem $Work -Filter "agent-*.log" -ErrorAction SilentlyContinue | Remove-Item

if (-not $Exe) {
    $Exe = Join-Path $Work "winuse.exe"
    Say "building winuse into $Exe"
    Push-Location $Repo
    try {
        $env:CGO_ENABLED = "0"
        & go build -o $Exe ./cmd/winuse
        if ($LASTEXITCODE -ne 0) { Fail "go build failed" }
    } finally { Pop-Location }
}
$Exe = (Resolve-Path $Exe).Path

$ids = @{}
$ffmpeg = $null
$jobs = @()
$profileWasThere = Test-Path $DemoProfile
$failed = $null
try {
    $helperPid = Start-CaptureHelper

    # Geometry: the rectangle centred on the primary display.
    $disp = Invoke-Winuse @("list-displays")
    $prim = $disp -split "`r?`n" | Where-Object { $_ -match "primary" } | Select-Object -First 1
    if ($prim -notmatch "(-?\d+),(-?\d+) (\d+)x(\d+)") { Fail "cannot read the primary display: $disp" }
    $dx, $dy, $dw, $dh = [int]$Matches[1], [int]$Matches[2], [int]$Matches[3], [int]$Matches[4]
    if ($dw -lt $RecW + 80 -or $dh -lt $RecH + 130) { Fail "the primary display ${dw}x${dh} is too small for ${RecW}x${RecH} (it wants a 2560x1440 display or larger)" }
    $X = $dx + [int](($dw - $RecW) / 2)
    # Centred, but at least 110 px above the display's bottom: the taskbar.
    $Y = $dy + [Math]::Min([int](($dh - $RecH) / 2), $dh - $RecH - 110)
    Say "recording ${RecW}x${RecH} at $X,$Y"

    # Chrome on winuse's own launch profile; the port file is how winuse
    # finds the Browser Connection of a Chrome it launched.
    $port = Get-FreePort
    New-Item -ItemType Directory -Force $DemoProfile | Out-Null
    Set-Content -Path (Join-Path $DemoProfile "winuse-port") -Value "$port" -NoNewline -Encoding ascii
    Start-DemoWindow "backdrop.html" $port @()
    $ids.backdrop = Wait-DemoWindow "winuse demo backdrop"
    # The backdrop overhangs the rectangle; its title bar is above it.
    Place-Window $ids.backdrop ($X - 40) ($Y - 90) ($RecW + 80) ($RecH + 100)

    $m = 30
    $w = [int](($RecW - 4 * $m) / 3)
    $h = $RecH - $m - 70
    $i = 0
    foreach ($a in $Agents) {
        $page = @{ "Pixel studio" = "pixel.html"; "Launch board" = "board.html"; "Release notes" = "notes.html" }[$a.title]
        Start-DemoWindow $page $port @()
        $ids[$a.name] = Wait-DemoWindow $a.title
        Place-Window $ids[$a.name] ($X + $m + $i * ($w + $m)) ($Y + $m) $w $h
        $i++
    }
    Start-Sleep -Milliseconds 800 # the pages settle at their new size

    # The Agents read their windows and wait for the go.
    $go = Join-Path $Work "go.flag"
    foreach ($a in $Agents) {
        $ready = Join-Path $Work "ready-$($a.name).flag"
        $log = Join-Path $Work "agent-$($a.name).log"
        $json = ConvertTo-Json -Compress -Depth 4 @($a.steps)
        $jobs += Start-Job -Name "demo-$($a.name)" -ScriptBlock $AgentJob -ArgumentList $Exe, $ids[$a.name], $a.name, $a.color, $json, $a.pause, $ready, $go, $log
    }
    $deadline = (Get-Date).AddSeconds(60)
    while (@($Agents | Where-Object { -not (Test-Path (Join-Path $Work "ready-$($_.name).flag")) }).Count -gt 0) {
        foreach ($j in $jobs) { if ($j.State -eq "Failed") { Fail "agent $($j.Name) failed before the go: $(Receive-Job $j 2>&1 | Out-String)" } }
        if ((Get-Date) -gt $deadline) { Fail "the Agents did not read their windows within 60 s" }
        Start-Sleep -Milliseconds 100
    }

    # Record.
    Remove-Item $Raw -ErrorAction SilentlyContinue
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = (Get-Command ffmpeg).Source
    $enc = "-c:v libx264 -preset ultrafast -crf 12 -pix_fmt yuv444p"
    if ($Grabber -eq "ddagrab") {
        $psi.Arguments = "-hide_banner -loglevel error -nostats -y -f lavfi -i `"ddagrab=output_idx=0:framerate=30:draw_mouse=0:offset_x=$($X - $dx):offset_y=$($Y - $dy):video_size=${RecW}x${RecH}`" -vf hwdownload,format=bgra $enc `"$Raw`""
    } else {
        # gdigrab is not DPI-aware on its own: the compatibility layer gives
        # it physical pixels on a scaled display.
        $psi.EnvironmentVariables["__COMPAT_LAYER"] = "HighDpiAware"
        $psi.Arguments = "-hide_banner -loglevel error -nostats -y -f gdigrab -framerate 30 -draw_mouse 0 -offset_x $X -offset_y $Y -video_size ${RecW}x${RecH} -i desktop $enc `"$Raw`""
    }
    $psi.UseShellExecute = $false
    $psi.RedirectStandardInput = $true
    Say "recording with $Grabber"
    $ffmpeg = [System.Diagnostics.Process]::Start($psi)
    Start-Sleep -Milliseconds 1500
    if ($ffmpeg.HasExited) { Fail "ffmpeg exited at once (exit $($ffmpeg.ExitCode))" }

    Say "go: artist, planner and writer act at once"
    Set-Content -Path $go -Value "go"
    $t0 = Get-Date
    Wait-Job $jobs -Timeout 120 | Out-Null
    foreach ($j in $jobs) {
        if ($j.State -ne "Completed") {
            Stop-Job $j -ErrorAction SilentlyContinue
            Fail "agent $($j.Name) $($j.State): $(Receive-Job $j 2>&1 | Out-String)"
        }
    }
    Say ("the Agents finished in {0:N1} s" -f ((Get-Date) - $t0).TotalSeconds)
    Start-Sleep -Seconds 3 # the finished windows, all three cursors at rest

    $ffmpeg.StandardInput.Write("q")
    $ffmpeg.StandardInput.Close()
    if (-not $ffmpeg.WaitForExit(15000)) { $ffmpeg.Kill(); Fail "ffmpeg did not stop" }
    if ($ffmpeg.ExitCode -ne 0) { Fail "ffmpeg exited with $($ffmpeg.ExitCode)" }
    $ffmpeg = $null
    if ((Get-Size $Raw) -ne "${RecW}x${RecH}") { Fail "the recording is $(Get-Size $Raw), not ${RecW}x${RecH}" }

    $nowPid = Get-HelperPid
    if ($nowPid -ne $helperPid) {
        Fail "the helper changed during the recording (pid $helperPid, now $nowPid): another winuse client of a different build replaced it, so the cursors may be missing. Run again with nothing else using winuse."
    }
} catch {
    $failed = $_
} finally {
    # Each cleanup step on its own: one that fails is reported and the
    # rest still run, the helper's restore above all.
    $cleanup = {
        param([string]$What, [scriptblock]$Step)
        try { & $Step } catch { Write-Warning "cleanup: $What failed: $_" }
    }
    & $cleanup "stopping ffmpeg" {
        if ($ffmpeg -and -not $ffmpeg.HasExited) {
            try { $ffmpeg.StandardInput.Write("q"); $ffmpeg.StandardInput.Close() } catch {}
            if (-not $ffmpeg.WaitForExit(10000)) { $ffmpeg.Kill() }
        }
    }
    & $cleanup "stopping the Agent jobs" {
        foreach ($j in $jobs) { Stop-Job $j -ErrorAction SilentlyContinue; Remove-Job $j -Force -ErrorAction SilentlyContinue }
    }
    & $cleanup "closing the demo windows" {
        Say "closing the demo windows"
        Close-DemoWindows @($ids.Values)
    }
    & $cleanup "removing the demo profile" {
        if (-not $profileWasThere -and (Test-Path $DemoProfile)) {
            Start-Sleep -Milliseconds 500
            Remove-Item -Recurse -Force $DemoProfile -ErrorAction SilentlyContinue
        }
    }
    & $cleanup "restoring the helper" { Restore-Helper }
    & $cleanup "removing the flags" { Get-ChildItem $Work -Filter "*.flag" -ErrorAction SilentlyContinue | Remove-Item }
}
if ($failed) {
    Write-Host "Agent logs: $Work\agent-*.log"
    throw $failed
}

Encode-Demo $Raw $Gif $Mp4 1.5
Say "done: $Gif and $Mp4 (raw: $Raw)"
