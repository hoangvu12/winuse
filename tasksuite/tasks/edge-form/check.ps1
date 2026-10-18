# Check: edge-form. The form's submitted values must have reached the
# task server (submissions.txt, one JSON line per submission). The last
# submission decides: retries are allowed, a wrong final state is not.
$scratch = $env:WINUSE_TASK_SCRATCH
$log = Join-Path $scratch 'submissions.txt'
function Fail($message) { Write-Output $message; exit 1 }

if (-not (Test-Path -LiteralPath $log)) { Fail 'the form was never submitted (no submission reached the task server)' }
$lines = @(Get-Content -LiteralPath $log | Where-Object { $_.Trim() -ne '' })
if ($lines.Count -eq 0) { Fail 'the form was never submitted (no submission reached the task server)' }

try {
    $data = $lines[$lines.Count - 1] | ConvertFrom-Json
} catch {
    Fail 'the recorded submission is not valid JSON'
}

$problems = @()
if ($data.name -ne 'Marina Ionescu') { $problems += 'full name' }
if ($data.email -ne 'marina.ionescu@example.org') { $problems += 'email' }
if ($data.team -ne 'Cascade') { $problems += 'team' }
if ($data.role -ne 'Operations') { $problems += 'role' }
if ($data.newsletter -isnot [bool] -or -not $data.newsletter) { $problems += 'newsletter checkbox' }
if ($data.notes -ne 'I can bring a spare keyboard to the meetup.') { $problems += 'notes' }

if ($problems.Count -gt 0) {
    Fail ("the submitted form values are wrong: " + ($problems -join ', '))
}
exit 0
