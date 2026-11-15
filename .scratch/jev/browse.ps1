# Read pages in the agent's own Brave tab for the Jev provider research.
#   browse.ps1 -Tab 5902020:a269b680 -Url https://... [-All]
param([string]$Tab, [string[]]$Url, [switch]$All, [int]$Max = 2500)
$w = "$env:TEMP\winuse-jev-dev\winuse.exe"
$filter = if ($All) { 'true' } else { '/free|credit|trial|base.?url|systemone|endpoint|\$|price|pricing|quota|sign ?up|jev|openjev|model/i.test(l)' }
foreach ($u in $Url) {
  "===== $u"
  & $w navigate $Tab $u 2>&1 | Select-Object -First 2
  $js = "document.body.innerText.replace(/\n{2,}/g,'\n').split('\n').map(s=>s.trim()).filter(l=>l && ($filter)).join('\n').slice(0,$Max)"
  & $w eval $Tab $js 2>&1
}
