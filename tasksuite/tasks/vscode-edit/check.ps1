# Check: vscode-edit. checklist.md must exist in the project folder with
# exactly the required text.
$dir = Join-Path $env:WINUSE_TASK_SCRATCH 'project'
function Fail($message) { Write-Output $message; exit 1 }

$f = Join-Path $dir 'checklist.md'
if (-not (Test-Path -LiteralPath $f -PathType Leaf)) { Fail 'checklist.md was not created in the project folder' }
$actual = [System.IO.File]::ReadAllText($f)
$expected = [System.IO.File]::ReadAllText((Join-Path $dir 'expected.txt'))
$norm = { param($s) ($s -replace "`r`n", "`n").TrimEnd("`n") }
if ((& $norm $actual) -ne (& $norm $expected)) {
    Fail 'checklist.md does not contain exactly the required text'
}
exit 0
