# Task setup: notepad-note. Everything is written inside the task's
# scratch folder ($env:WINUSE_TASK_SCRATCH): the expected note text, for
# the Check to compare against.
$scratch = $env:WINUSE_TASK_SCRATCH
New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$note = "Museum visit notes`n`nThe natural history museum opens at 9:30 on weekdays.`nThe planetarium show costs 12 euros and runs at 11:00 and 14:00.`nRemember: no flash photography in the gem hall.`n`nTodo after the visit: buy the guidebook and email Anna the photos.`n"
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $scratch 'expected.txt'), $note, $utf8)
exit 0
