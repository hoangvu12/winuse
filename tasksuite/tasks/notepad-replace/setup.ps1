# Task setup: notepad-replace. Writes the project log with three DRAFT
# markers and the expected final text.
$scratch = $env:WINUSE_TASK_SCRATCH
New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$before = "Project log - week 12`n`nDRAFT: fixed the login timeout on mobile.`nDRAFT: replaced the cache layer; metrics look stable.`nDRAFT: scheduled the release review for Friday.`n`nNothing else this week.`n"
$after = $before.Replace('DRAFT', 'FINAL')
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $scratch 'project-log.txt'), $before, $utf8)
[System.IO.File]::WriteAllText((Join-Path $scratch 'expected.txt'), $after, $utf8)
exit 0
