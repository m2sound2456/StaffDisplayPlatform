# =============================================================================
# generate-icons.ps1 — regenerate the PWA / favicon icon set (no external deps)
# =============================================================================
# Usage: pwsh ./scripts/generate-icons.ps1
# Output: frontend/public/icons/*.png + frontend/public/favicon.svg
#
# Brand: dark slate rounded square with a "staff card" glyph (avatar + card).
# Keep this script in sync with the design tokens in frontend/src/styles/index.css.
# =============================================================================
[CmdletBinding()]
param(
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $repoRoot "frontend/public/icons" }
$publicDir = Split-Path -Parent $OutDir
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$bgColor     = [System.Drawing.ColorTranslator]::FromHtml("#0f172a")
$accentColor = [System.Drawing.ColorTranslator]::FromHtml("#22d3ee")
$fgColor     = [System.Drawing.ColorTranslator]::FromHtml("#f8fafc")

function New-Icon {
    param([int]$Size, [string]$FileName, [double]$Padding = 0.14)

    $bmp = New-Object System.Drawing.Bitmap($Size, $Size)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.Clear([System.Drawing.Color]::Transparent)
    $g.FillRectangle((New-Object System.Drawing.SolidBrush($bgColor)), 0, 0, $Size, $Size)

    $inset = [int]($Size * $Padding)
    $cardW = $Size - (2 * $inset)
    $cardH = [int]($cardW * 0.72)
    $cardX = $inset
    $cardY = [int](($Size - $cardH) / 2)
    $radius = [int]($Size * 0.06)

    # Card body
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $d = $radius * 2
    $path.AddArc($cardX, $cardY, $d, $d, 180, 90)
    $path.AddArc($cardX + $cardW - $d, $cardY, $d, $d, 270, 90)
    $path.AddArc($cardX + $cardW - $d, $cardY + $cardH - $d, $d, $d, 0, 90)
    $path.AddArc($cardX, $cardY + $cardH - $d, $d, $d, 90, 90)
    $path.CloseFigure()
    $g.FillPath((New-Object System.Drawing.SolidBrush($fgColor)), $path)

    # Avatar (accent circle) + shoulders
    $avatarD = [int]($cardH * 0.46)
    $avatarX = $cardX + [int]($cardW * 0.10)
    $avatarY = $cardY + [int](($cardH - $avatarD) / 2)
    $g.FillEllipse((New-Object System.Drawing.SolidBrush($accentColor)), $avatarX, $avatarY, $avatarD, $avatarD)
    $shoulderW = [int]($avatarD * 1.5)
    $shoulderH = [int]($avatarD * 0.8)
    $g.FillEllipse(
        (New-Object System.Drawing.SolidBrush($accentColor)),
        ($avatarX + [int](($avatarD - $shoulderW) / 2)),
        ($avatarY + [int]($avatarD * 0.72)),
        $shoulderW,
        $shoulderH
    )

    # Name + status lines
    $lineX = $avatarX + $avatarD + [int]($cardW * 0.08)
    $lineW = ($cardX + $cardW) - $lineX - [int]($cardW * 0.10)
    $lineH = [Math]::Max(2, [int]($cardH * 0.11))
    $g.FillRectangle((New-Object System.Drawing.SolidBrush($bgColor)), $lineX, ($cardY + [int]($cardH * 0.28)), $lineW, $lineH)
    $g.FillRectangle((New-Object System.Drawing.SolidBrush($accentColor)), $lineX, ($cardY + [int]($cardH * 0.55)), [int]($lineW * 0.55), $lineH)

    $target = Join-Path $OutDir $FileName
    $bmp.Save($target, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose()
    $bmp.Dispose()
    Write-Host "  wrote $target"
}

Write-Host "Generating PWA icons into $OutDir"
New-Icon -Size 192  -FileName "icon-192.png"
New-Icon -Size 512  -FileName "icon-512.png"
New-Icon -Size 512  -FileName "icon-maskable-512.png" -Padding 0.24
New-Icon -Size 180  -FileName "apple-touch-icon.png"
New-Icon -Size 32   -FileName "favicon-32.png"

$svg = @'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="Staff Display Platform">
  <rect width="64" height="64" rx="12" fill="#0f172a"/>
  <rect x="9" y="17" width="46" height="30" rx="4" fill="#f8fafc"/>
  <circle cx="22" cy="29" r="7" fill="#22d3ee"/>
  <ellipse cx="22" cy="42" rx="10" ry="5" fill="#22d3ee"/>
  <rect x="34" y="26" width="16" height="4" rx="2" fill="#0f172a"/>
  <rect x="34" y="36" width="9" height="4" rx="2" fill="#22d3ee"/>
</svg>
'@
Set-Content -Path (Join-Path $publicDir "favicon.svg") -Value $svg -Encoding UTF8
Write-Host "  wrote $(Join-Path $publicDir 'favicon.svg')"
Write-Host "Done."
