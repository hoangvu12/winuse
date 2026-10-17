# Check: explorer-organize. Inspects the end state of the Invoices
# folder only: the exact layout (2024 folder, notes.txt, summary.txt),
# the six invoices inside 2024 with their original contents, and the
# renamed summary with its original content.
$dir = Join-Path $env:WINUSE_TASK_SCRATCH 'Invoices'

function Fail($message) { Write-Output $message; exit 1 }
$utf8 = New-Object System.Text.UTF8Encoding($false)

if (-not (Test-Path -LiteralPath $dir -PathType Container)) { Fail 'the Invoices folder is missing' }

# The root must contain exactly the 2024 folder, notes.txt and
# summary.txt — nothing left over, nothing extra.
$root = @(Get-ChildItem -LiteralPath $dir | ForEach-Object { $_.Name }) | Sort-Object
$wantRoot = @('2024', 'notes.txt', 'summary.txt')
$rootDiff = Compare-Object $root $wantRoot
if ($rootDiff) { Fail "the Invoices folder layout is wrong: expected 2024, notes.txt, summary.txt; found $($root -join ', ')" }

$inner = Join-Path $dir '2024'
$files = @(Get-ChildItem -LiteralPath $inner -File | ForEach-Object { $_.Name }) | Sort-Object
$wantFiles = @('invoice-2024-01.txt', 'invoice-2024-02.txt', 'invoice-2024-03.txt', 'invoice-2024-04.txt', 'invoice-2024-05.txt', 'invoice-2024-06.txt')
if ((Compare-Object $files $wantFiles)) {
    Fail "the 2024 folder does not contain exactly the six invoice files: found $($files -join ', ')"
}

# Each invoice keeps its original content.
1..6 | ForEach-Object {
    $n = '{0:d2}' -f $_
    $want = "Invoice 2024-$n`nAmount due: $($_ * 25).00 EUR`nStatus: open`n"
    $got = [System.IO.File]::ReadAllText((Join-Path $inner "invoice-2024-$n.txt"))
    if ((($got -replace "`r`n", "`n").TrimEnd("`n")) -ne (($want -replace "`r`n", "`n").TrimEnd("`n"))) {
        Fail "invoice-2024-$n.txt lost or changed its content"
    }
}

# summary.txt is the renamed draft-summary, content untouched.
$summary = [System.IO.File]::ReadAllText((Join-Path $dir 'summary.txt'))
if ($summary -notmatch 'Draft summary: six invoices, no late payments\.') {
    Fail 'summary.txt does not hold the original draft-summary text'
}
$notes = [System.IO.File]::ReadAllText((Join-Path $dir 'notes.txt'))
if ($notes -notmatch 'Call the accountant about the VAT number\.') {
    Fail 'notes.txt is missing or changed'
}
exit 0
