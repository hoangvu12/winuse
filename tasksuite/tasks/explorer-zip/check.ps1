# Check: explorer-zip. reports.zip must be a ZIP holding exactly the
# three report files with their original contents, and the originals
# must still be in the folder.
$dir = Join-Path $env:WINUSE_TASK_SCRATCH 'Reports'
function Fail($message) { Write-Output $message; exit 1 }

Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

$zipPath = Join-Path $dir 'reports.zip'
if (-not (Test-Path -LiteralPath $zipPath -PathType Leaf)) { Fail 'reports.zip was not created in the Reports folder' }

$expected = @{
    'field-report.txt'   = "Field report A`nSite: northern ridge`nWeather: clear, 14 C`n"
    'measurements.txt'   = "Measurements`nplot-1: 42`nplot-2: 55`nplot-3: 61`n"
    'photos-index.txt'   = "Photo index`nIMG_0401: ridge sunrise`nIMG_0402: sample tray`n"
}

$zip = [System.IO.Compression.ZipFile]::OpenRead($zipPath)
try {
    $names = @($zip.Entries | ForEach-Object { [System.IO.Path]::GetFileName($_.FullName) }) | Sort-Object
    $want = @($expected.Keys) | Sort-Object
    if ((Compare-Object $names $want)) {
        Fail "reports.zip does not contain exactly the three report files: found $($names -join ', ')"
    }
    foreach ($entry in $zip.Entries) {
        $name = [System.IO.Path]::GetFileName($entry.FullName)
        $reader = New-Object System.IO.StreamReader($entry.Open(), (New-Object System.Text.UTF8Encoding($false)))
        try { $got = $reader.ReadToEnd() } finally { $reader.Dispose() }
        $wantText = $expected[$name]
        if ((($got -replace "`r`n", "`n").TrimEnd("`n")) -ne (($wantText -replace "`r`n", "`n").TrimEnd("`n"))) {
            Fail "$name inside reports.zip lost or changed its content"
        }
    }
} finally {
    $zip.Dispose()
}

foreach ($name in @('field-report.txt', 'measurements.txt', 'photos-index.txt')) {
    if (-not (Test-Path -LiteralPath (Join-Path $dir $name) -PathType Leaf)) {
        Fail "the original $name is no longer in the Reports folder"
    }
}
exit 0
