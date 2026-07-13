#requires -Version 5.1

[CmdletBinding()]
param(
    [switch]$Check
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$sourcePath = Join-Path $repoRoot "cmd\twintidy\winres\icon.png"
$assetDirectory = Join-Path $repoRoot "packaging\msix\Assets"
$assetSizes = [ordered]@{
    "Square44x44Logo.png" = 44
    "Square150x150Logo.png" = 150
    "StoreLogo.png" = 50
}

if (-not [System.IO.File]::Exists($sourcePath)) {
    throw "TwinTidy icon source is missing: $sourcePath"
}

Add-Type -AssemblyName System.Drawing

function Write-TwinTidyMSIXAsset {
    param(
        [Parameter(Mandatory = $true)][System.Drawing.Image]$Source,
        [Parameter(Mandatory = $true)][int]$Size,
        [Parameter(Mandatory = $true)][string]$Path
    )

    $bitmap = [System.Drawing.Bitmap]::new($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $bitmap.SetResolution(96, 96)
        $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
        try {
            $graphics.Clear([System.Drawing.Color]::Transparent)
            $graphics.CompositingMode = [System.Drawing.Drawing2D.CompositingMode]::SourceCopy
            $graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
            $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
            $graphics.DrawImage($Source, [System.Drawing.Rectangle]::new(0, 0, $Size, $Size))
        } finally {
            $graphics.Dispose()
        }
        $bitmap.Save($Path, [System.Drawing.Imaging.ImageFormat]::Png)
    } finally {
        $bitmap.Dispose()
    }
}

$tempDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("TwinTidyMSIXAssets-" + [System.Guid]::NewGuid().ToString("N"))
[System.IO.Directory]::CreateDirectory($tempDirectory) | Out-Null
try {
    $source = [System.Drawing.Image]::FromFile($sourcePath)
    try {
        foreach ($asset in $assetSizes.GetEnumerator()) {
            Write-TwinTidyMSIXAsset -Source $source -Size $asset.Value -Path (Join-Path $tempDirectory $asset.Key)
        }
    } finally {
        $source.Dispose()
    }

    if ($Check) {
        foreach ($asset in $assetSizes.GetEnumerator()) {
            $tracked = Join-Path $assetDirectory $asset.Key
            if (-not [System.IO.File]::Exists($tracked)) {
                throw "Tracked MSIX asset is missing: $tracked"
            }
            $expected = (Get-FileHash -LiteralPath (Join-Path $tempDirectory $asset.Key) -Algorithm SHA256).Hash
            $actual = (Get-FileHash -LiteralPath $tracked -Algorithm SHA256).Hash
            if ($actual -cne $expected) {
                throw "MSIX asset '$($asset.Key)' is not the deterministic derivative of the TwinTidy icon."
            }
            [pscustomobject]@{ Asset = $asset.Key; Size = $asset.Value; Deterministic = $true; SHA256 = $actual }
        }
    } else {
        [System.IO.Directory]::CreateDirectory($assetDirectory) | Out-Null
        foreach ($asset in $assetSizes.GetEnumerator()) {
            [System.IO.File]::Copy((Join-Path $tempDirectory $asset.Key), (Join-Path $assetDirectory $asset.Key), $true)
        }
    }
} finally {
    $fullTemp = [System.IO.Path]::GetFullPath($tempDirectory)
    $systemTemp = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    if (-not $systemTemp.EndsWith([System.IO.Path]::DirectorySeparatorChar.ToString())) {
        $systemTemp += [System.IO.Path]::DirectorySeparatorChar
    }
    if (-not $fullTemp.StartsWith($systemTemp, [System.StringComparison]::OrdinalIgnoreCase) -or
        -not [System.IO.Path]::GetFileName($fullTemp).StartsWith("TwinTidyMSIXAssets-", [System.StringComparison]::Ordinal)) {
        throw "Refusing to remove unexpected MSIX asset directory: $fullTemp"
    }
    if ([System.IO.Directory]::Exists($fullTemp)) {
        [System.IO.Directory]::Delete($fullTemp, $true)
    }
}
