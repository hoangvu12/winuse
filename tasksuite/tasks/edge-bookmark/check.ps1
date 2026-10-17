# Check: edge-bookmark. The bookmark must exist in the isolated profile
# (in the scratch folder) with the exact name and URL.
$scratch = $env:WINUSE_TASK_SCRATCH
function Fail($message) { Write-Output $message; exit 1 }

$bmPath = Join-Path $scratch 'edge-profile\Default\Bookmarks'
if (-not (Test-Path -LiteralPath $bmPath)) { Fail 'the bookmark was never saved (the profile has no Bookmarks file)' }

$wantUrl = "$($env:WINUSE_TASK_URL)/page-b.html"
try {
    $bookmarks = (Get-Content -LiteralPath $bmPath -Encoding UTF8 -Raw) | ConvertFrom-Json
} catch {
    Fail 'the Edge profile Bookmarks file could not be parsed'
}

function Find-Bookmark($node, $name, $url) {
    if ($node.type -eq 'url' -and $node.name -eq $name -and $node.url -eq $url) { return $true }
    if ($node.children) {
        foreach ($child in $node.children) {
            if (Find-Bookmark $child $name $url) { return $true }
        }
    }
    return $false
}

$found = $false
foreach ($rootName in @('bookmark_bar', 'other', 'synced')) {
    $root = $bookmarks.$rootName
    if ($null -ne $root) {
        if (Find-Bookmark $root 'Field guide' $wantUrl) { $found = $true }
    }
}
if (-not $found) {
    Fail "no bookmark named 'Field guide' pointing at $wantUrl was found in the profile"
}
exit 0
