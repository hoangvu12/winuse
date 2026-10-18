# Check: paint-draw. line.png must be a valid image containing a real
# drawn line: at least 100 non-white pixels (a pencil line across the
# default canvas is hundreds of pixels long).
$scratch = $env:WINUSE_TASK_SCRATCH
function Fail($message) { Write-Output $message; exit 1 }

$f = Join-Path $scratch 'line.png'
if (-not (Test-Path -LiteralPath $f -PathType Leaf)) { Fail 'line.png was not created in the task folder' }

Add-Type -AssemblyName System.Drawing
try {
    $bmp = [System.Drawing.Bitmap]::FromFile($f)
} catch {
    Fail 'line.png is not a readable image'
}
try {
    if ($bmp.Width -lt 200 -or $bmp.Height -lt 200) {
        Fail "line.png is suspiciously small ($($bmp.Width)x$($bmp.Height))"
    }
    $rect = New-Object System.Drawing.Rectangle(0, 0, $bmp.Width, $bmp.Height)
    $bd = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $bytes = New-Object byte[] ($bd.Stride * $bd.Height)
    [System.Runtime.InteropServices.Marshal]::Copy($bd.Scan0, $bytes, 0, $bytes.Length)
    $bmp.UnlockBits($bd)

    $nonWhite = 0
    for ($i = 0; $i -lt $bytes.Length; $i += 4) {
        if ($bytes[$i] -ne 255 -or $bytes[$i + 1] -ne 255 -or $bytes[$i + 2] -ne 255) { $nonWhite++ }
    }
    if ($nonWhite -lt 100) {
        Fail "line.png has only $nonWhite non-white pixels (expected at least 100: no visible line was drawn)"
    }
} finally {
    $bmp.Dispose()
}
exit 0
