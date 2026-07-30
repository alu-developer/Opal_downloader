#requires -Version 5.1
<#
Renders internal/gui's logoSVG mark into a multi-size Windows .ico and checks
it into internal/gui/assets/icon.ico.

Why WPF and not an SVG library: the blocker recorded in docs/BACKLOG.md ("needs
an SVG renderer") assumed one had to be added as a dependency. It doesn't -
System.Windows.Media.Geometry.Parse already understands the SVG path
mini-language (M/H/L/A/Z, and it defaults to the same even-odd fill rule the
source SVG uses), so WPF - present in every .NET Framework on this machine -
rasterises the exact mark with no new dependency and nothing added to go.mod.

The geometry below (rect + gradient + path) is transcribed by hand from
logoSVG in internal/gui/chrome.go. If that constant changes, this script's
$rectRadius/$gradientStops/$pathData must change with it - there is no
automatic link between the two, since Go doesn't have an SVG parser to read
logoSVG from here either. Rerun this script after any logo edit.
#>

Add-Type -AssemblyName PresentationCore, PresentationFramework, WindowsBase

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$outDir = Join-Path $repoRoot 'internal\gui\assets'
$outFile = Join-Path $outDir 'icon.ico'
$sizes = @(16, 32, 48, 64, 128, 256)

# --- geometry, transcribed from logoSVG (viewBox 0 0 64 64) -------------------
$pathData = 'M20 14H34a18 18 0 0 1 0 36H20Z M27 24H45L36 42Z'
$gradientStops = @(
    @{ Offset = 0.00; Color = '#3d8bfd' }
    @{ Offset = 0.45; Color = '#7b5cff' }
    @{ Offset = 0.75; Color = '#e15fd0' }
    @{ Offset = 1.00; Color = '#2fd6c3' }
)
$rectRadius = 14.0

function New-LogoPng([int]$size) {
    $rectGeom = New-Object System.Windows.Media.RectangleGeometry(
        (New-Object System.Windows.Rect(0, 0, 64, 64)), $rectRadius, $rectRadius)

    $brush = New-Object System.Windows.Media.LinearGradientBrush
    $brush.MappingMode = [System.Windows.Media.BrushMappingMode]::RelativeToBoundingBox
    $brush.StartPoint = New-Object System.Windows.Point(0, 0)
    $brush.EndPoint = New-Object System.Windows.Point(1, 1)
    foreach ($stop in $gradientStops) {
        $color = [System.Windows.Media.ColorConverter]::ConvertFromString($stop.Color)
        $brush.GradientStops.Add((New-Object System.Windows.Media.GradientStop($color, $stop.Offset)))
    }

    # Geometry.Parse's default fill rule is EvenOdd, matching the source
    # SVG's fill-rule="evenodd" - the triangular counter relies on that to
    # read as a hole rather than a solid overlapping shape.
    $pathGeom = [System.Windows.Media.Geometry]::Parse($pathData)

    $group = New-Object System.Windows.Media.DrawingGroup
    $group.Transform = New-Object System.Windows.Media.ScaleTransform(($size / 64.0), ($size / 64.0))
    $group.Children.Add((New-Object System.Windows.Media.GeometryDrawing($brush, $null, $rectGeom)))
    $group.Children.Add((New-Object System.Windows.Media.GeometryDrawing([System.Windows.Media.Brushes]::White, $null, $pathGeom)))

    $visual = New-Object System.Windows.Media.DrawingVisual
    $dc = $visual.RenderOpen()
    $dc.DrawDrawing($group)
    $dc.Close()

    $rtb = New-Object System.Windows.Media.Imaging.RenderTargetBitmap(
        $size, $size, 96, 96, [System.Windows.Media.PixelFormats]::Pbgra32)
    $rtb.Render($visual)

    $encoder = New-Object System.Windows.Media.Imaging.PngBitmapEncoder
    $encoder.Frames.Add([System.Windows.Media.Imaging.BitmapFrame]::Create($rtb))
    $ms = New-Object System.IO.MemoryStream
    $encoder.Save($ms)
    return , $ms.ToArray()
}

# --- pack the PNG frames into one multi-size .ico container -------------------
# Storing each frame as PNG (rather than a raw DIB) is the documented ICO
# format since Windows Vista and is what every size here, including 16px,
# resolves through: LoadImage/ExtractIcon/the shell all decode it, which is
# also the runtime loading path this repo uses (see window_windows.go).
$pngFrames = foreach ($size in $sizes) { New-LogoPng $size }

$dirHeaderSize = 6
$entrySize = 16
$offset = $dirHeaderSize + ($entrySize * $sizes.Count)

$ms = New-Object System.IO.MemoryStream
$bw = New-Object System.IO.BinaryWriter($ms)

$bw.Write([uint16]0)      # reserved
$bw.Write([uint16]1)      # type: 1 = icon
$bw.Write([uint16]$sizes.Count)

for ($i = 0; $i -lt $sizes.Count; $i++) {
    $size = $sizes[$i]
    $png = $pngFrames[$i]
    $dim = if ($size -ge 256) { 0 } else { $size }  # 0 means 256 in the ICO format
    $bw.Write([byte]$dim)       # width
    $bw.Write([byte]$dim)       # height
    $bw.Write([byte]0)          # color count (0 = not a palette image)
    $bw.Write([byte]0)          # reserved
    $bw.Write([uint16]1)        # color planes
    $bw.Write([uint16]32)       # bits per pixel
    $bw.Write([uint32]$png.Length)
    $bw.Write([uint32]$offset)
    $offset += $png.Length
}
foreach ($png in $pngFrames) { $bw.Write($png) }
$bw.Flush()

if (-not (Test-Path $outDir)) { New-Item -ItemType Directory -Path $outDir -Force | Out-Null }
[System.IO.File]::WriteAllBytes($outFile, $ms.ToArray())
$bw.Dispose()
$ms.Dispose()

Write-Output "Wrote $outFile ($($sizes -join ', ') px, $((Get-Item $outFile).Length) bytes)"
