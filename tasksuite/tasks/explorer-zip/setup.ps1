# Task setup: explorer-zip. Creates the Reports folder with three report
# files whose contents the Check regenerates from the same rules.
$scratch = $env:WINUSE_TASK_SCRATCH
$dir = Join-Path $scratch 'Reports'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $dir 'field-report.txt'), "Field report A`nSite: northern ridge`nWeather: clear, 14 C`n", $utf8)
[System.IO.File]::WriteAllText((Join-Path $dir 'measurements.txt'), "Measurements`nplot-1: 42`nplot-2: 55`nplot-3: 61`n", $utf8)
[System.IO.File]::WriteAllText((Join-Path $dir 'photos-index.txt'), "Photo index`nIMG_0401: ridge sunrise`nIMG_0402: sample tray`n", $utf8)
exit 0
