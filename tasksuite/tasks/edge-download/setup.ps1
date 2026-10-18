# Task setup: edge-download. Writes the download page and the file it
# links; the page is served by the task server from this scratch folder.
$scratch = $env:WINUSE_TASK_SCRATCH
New-Item -ItemType Directory -Force -Path $scratch | Out-Null

$html = @'
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Shared files</title>
<style>
  body { font: 16px system-ui, sans-serif; margin: 60px auto; max-width: 520px; }
  a { font-size: 18px; }
</style>
</head>
<body>
<h1>Shared files</h1>
<p>The quarterly report is available here:</p>
<p><a id="report" href="/file.bin" download>Download the report (file.bin)</a></p>
</body>
</html>
'@

$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $scratch 'download.html'), $html, $utf8)

$payload = [System.Text.Encoding]::ASCII.GetBytes(("winuse quarterly report payload`n" * 80))
[System.IO.File]::WriteAllBytes((Join-Path $scratch 'file.bin'), $payload)
exit 0
