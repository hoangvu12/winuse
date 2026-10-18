# Check: notepad-note. The note must exist with exactly the required
# text. Line endings and any trailing newline are normalized, so any
# Notepad generation's save behaviour passes; the words must match.
$scratch = $env:WINUSE_TASK_SCRATCH
$f = Join-Path $scratch 'museum-notes.txt'
if (-not (Test-Path -LiteralPath $f)) {
    Write-Output 'museum-notes.txt was not created in the task folder'
    exit 1
}
$actual = [System.IO.File]::ReadAllText($f)
$expected = [System.IO.File]::ReadAllText((Join-Path $scratch 'expected.txt'))
$norm = { param($s) ($s -replace "`r`n", "`n").TrimEnd("`n") }
if ((& $norm $actual) -ne (& $norm $expected)) {
    Write-Output 'museum-notes.txt does not contain exactly the required note text'
    exit 1
}
exit 0
