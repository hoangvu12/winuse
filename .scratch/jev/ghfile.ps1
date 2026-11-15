# Save files of a GitHub repo under .scratch/jev/src/<owner>_<repo>/,
# fetched through api.github.com inside the agent's own Brave tab.
#   ghfile.ps1 -Tab 5902020:a269b680 -Repo browser-use/jev-ultrafast -Path README.md,docs/design.md
param([string]$Tab, [string]$Repo, [string[]]$Path, [string]$Ref = 'main')
$w = "$env:TEMP\winuse-jev-dev\winuse.exe"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$root = Join-Path $PSScriptRoot ("src\" + ($Repo -replace '/', '_'))
$template = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'ghraw.js'))
$js = Join-Path $env:TEMP 'ghraw-run.js'
foreach ($p in $Path) {
  [IO.File]::WriteAllText($js, $template.Replace('__REPO__', $Repo).Replace('__PATH__', $p).Replace('__REF__', $Ref))
  & $w eval $Tab --file $js | Out-Null
  $text = $null
  for ($i = 0; $i -lt 40 -and -not $text; $i++) {
    $got = & $w eval $Tab "window.__gh['$p'] === null ? '' : window.__gh['$p']" --max 100000
    if (($got -join '') -ne '') { $text = $got } else { Start-Sleep -Milliseconds 250 }
  }
  $dest = Join-Path $root $p
  New-Item -ItemType Directory -Force (Split-Path $dest) | Out-Null
  [IO.File]::WriteAllText($dest, ($text -join "`n"))
  "{0}: {1} lines" -f $p, @($text).Count
}
