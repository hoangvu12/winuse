# Check: edge-download. Two end states: the downloaded file, byte for
# byte, and the browser setting (the profile lives in the scratch
# folder, so its Preferences file is in-scope task state).
$scratch = $env:WINUSE_TASK_SCRATCH
function Fail($message) { Write-Output $message; exit 1 }

# 1. The download, saved in the task folder with the exact bytes.
$source = Join-Path $scratch 'file.bin'
$saved = Join-Path $scratch 'report.bin'
if (-not (Test-Path -LiteralPath $saved -PathType Leaf)) { Fail 'report.bin was not saved in the task folder' }
$want = [System.IO.File]::ReadAllBytes($source)
$got = [System.IO.File]::ReadAllBytes($saved)
if ($got.Length -ne $want.Length) {
    Fail "report.bin is $($got.Length) bytes, expected $($want.Length)"
}
for ($i = 0; $i -lt $want.Length; $i++) {
    if ($got[$i] -ne $want[$i]) { Fail 'report.bin does not match the downloaded file byte for byte' }
}

# 2. The setting: "Ask where to save each file before downloading" must
# be on in the isolated profile's Preferences.
$prefPath = Join-Path $scratch 'edge-profile\Default\Preferences'
if (-not (Test-Path -LiteralPath $prefPath)) { Fail 'the isolated Edge profile has no Preferences file' }
try {
    $prefs = (Get-Content -LiteralPath $prefPath -Encoding UTF8 -Raw) | ConvertFrom-Json
} catch {
    Fail 'the Edge profile Preferences file could not be parsed'
}
$prompt = $false
if ($prefs.download -and $prefs.download.prompt_for_download) { $prompt = $true }
if (-not $prompt) {
    Fail '"Ask where to save each file before downloading" is not enabled in the profile'
}
exit 0
