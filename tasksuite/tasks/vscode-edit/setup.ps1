# Task setup: vscode-edit. Creates the project folder with one seed file
# and the expected checklist text.
$scratch = $env:WINUSE_TASK_SCRATCH
$dir = Join-Path $scratch 'project'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$checklist = "Setup checklist`n`n- confirm the display cable is seated`n- label the two spare SSDs`n- update the meeting room calendar`n"
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $dir 'seed.md'), "Project notes`n`nRoom B is booked for the hardware refresh.`n", $utf8)
[System.IO.File]::WriteAllText((Join-Path $dir 'expected.txt'), $checklist, $utf8)
exit 0
