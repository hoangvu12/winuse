# Task setup: edge-bookmark. Writes the two pages the task opens; they
# are served from the scratch folder by the task server.
$scratch = $env:WINUSE_TASK_SCRATCH
New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$pageA = @'
<!doctype html>
<html>
<head><meta charset="utf-8"><title>Field site log</title></head>
<body style="font: 16px system-ui, sans-serif; margin: 60px auto; max-width: 520px">
<h1>Field site log</h1>
<p>Observation post 3, northern ridge.</p>
<p>Conditions were clear for most of the week; two mornings of fog.</p>
</body>
</html>
'@
$pageB = @'
<!doctype html>
<html>
<head><meta charset="utf-8"><title>Field guide - birds</title></head>
<body style="font: 16px system-ui, sans-serif; margin: 60px auto; max-width: 520px">
<h1>Field guide: birds</h1>
<p>Common sightings at post 3: coal tit, Eurasian jay, grey wagtail.</p>
</body>
</html>
'@

$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $scratch 'page-a.html'), $pageA, $utf8)
[System.IO.File]::WriteAllText((Join-Path $scratch 'page-b.html'), $pageB, $utf8)
exit 0
