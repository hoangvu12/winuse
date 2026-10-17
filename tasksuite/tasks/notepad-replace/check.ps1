# Check: notepad-replace. Every DRAFT must have become FINAL, and the
# rest of the text must be unchanged (compared against the expected text
# the setup wrote).
$scratch = $env:WINUSE_TASK_SCRATCH
$f = Join-Path $scratch 'project-log.txt'
if (-not (Test-Path -LiteralPath $f)) {
    Write-Output 'project-log.txt is missing'
    exit 1
}
$actual = [System.IO.File]::ReadAllText($f)
$expected = [System.IO.File]::ReadAllText((Join-Path $scratch 'expected.txt'))
$norm = { param($s) ($s -replace "`r`n", "`n").TrimEnd("`n") }
$normActual = & $norm $actual
if ($normActual -cmatch 'DRAFT') {
    Write-Output 'project-log.txt still contains at least one DRAFT marker'
    exit 1
}
if ($normActual -ne (& $norm $expected)) {
    Write-Output 'project-log.txt differs from the expected replaced text'
    exit 1
}
exit 0
