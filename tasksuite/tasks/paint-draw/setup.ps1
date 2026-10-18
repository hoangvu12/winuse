# Task setup: paint-draw. The canvas task needs nothing but the scratch
# folder; the Check inspects the saved PNG.
$scratch = $env:WINUSE_TASK_SCRATCH
New-Item -ItemType Directory -Force -Path $scratch | Out-Null
exit 0
