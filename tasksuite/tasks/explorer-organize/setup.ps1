# Task setup: explorer-organize. Creates the Invoices folder with the
# six invoices, the notes and the draft summary. The Check regenerates
# the expected contents from the same rules, so no manifest file is
# needed.
$scratch = $env:WINUSE_TASK_SCRATCH
$dir = Join-Path $scratch 'Invoices'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$utf8 = New-Object System.Text.UTF8Encoding($false)
1..6 | ForEach-Object {
    $n = '{0:d2}' -f $_
    $content = "Invoice 2024-$n`nAmount due: $($_ * 25).00 EUR`nStatus: open`n"
    [System.IO.File]::WriteAllText((Join-Path $dir "invoice-2024-$n.txt"), $content, $utf8)
}
[System.IO.File]::WriteAllText((Join-Path $dir 'notes.txt'), "Call the accountant about the VAT number.`n", $utf8)
[System.IO.File]::WriteAllText((Join-Path $dir 'draft-summary.txt'), "Draft summary: six invoices, no late payments.`n", $utf8)
exit 0
