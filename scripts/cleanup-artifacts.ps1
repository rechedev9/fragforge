param(
    [string]$RunDir = "data\tries\faceit-288b1d33-martinez\run-004",
    [string]$KeepShortsDir = "shorts-natural-hq-full,shorts-natural-hq2-full",
    [switch]$Apply
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Fail {
    param([string]$Message)
    throw $Message
}

function Normalize-PathForCompare {
    param([string]$Path)
    return ([System.IO.Path]::GetFullPath($Path)).TrimEnd('\', '/')
}

function Test-PathInside {
    param(
        [string]$Child,
        [string]$Parent
    )
    $childFull = Normalize-PathForCompare $Child
    $parentFull = Normalize-PathForCompare $Parent
    if ($childFull.Equals($parentFull, [System.StringComparison]::OrdinalIgnoreCase)) {
        return $true
    }
    $parentWithSlash = $parentFull + [System.IO.Path]::DirectorySeparatorChar
    return $childFull.StartsWith($parentWithSlash, [System.StringComparison]::OrdinalIgnoreCase)
}

function Resolve-RunDir {
    param(
        [string]$Path,
        [string]$RepoRoot
    )
    if ([string]::IsNullOrWhiteSpace($Path)) {
        Fail "RunDir must not be empty."
    }
    $candidate = $Path
    if (-not [System.IO.Path]::IsPathRooted($candidate)) {
        $candidate = Join-Path $RepoRoot $candidate
    }
    if (-not (Test-Path -LiteralPath $candidate -PathType Container)) {
        Fail "RunDir does not exist or is not a directory: $candidate"
    }
    return (Resolve-Path -LiteralPath $candidate).Path
}

function Assert-SafeRunDir {
    param(
        [string]$Path,
        [string]$RepoRoot
    )
    $runFull = Normalize-PathForCompare $Path
    $repoFull = Normalize-PathForCompare $RepoRoot
    $rootFull = ([System.IO.Path]::GetPathRoot($runFull)).TrimEnd('\', '/')

    if (-not (Test-PathInside -Child $runFull -Parent $repoFull)) {
        Fail "Refusing to clean outside the repo. RunDir=$runFull Repo=$repoFull"
    }
    if ($runFull.Equals($rootFull, [System.StringComparison]::OrdinalIgnoreCase)) {
        Fail "Refusing to clean a drive root: $runFull"
    }

    $forbidden = @(
        $repoFull,
        (Normalize-PathForCompare (Join-Path $repoFull "data")),
        (Normalize-PathForCompare (Join-Path $repoFull "testdata"))
    )
    foreach ($blocked in $forbidden) {
        if ($runFull.Equals($blocked, [System.StringComparison]::OrdinalIgnoreCase)) {
            Fail "Refusing to clean protected path: $runFull"
        }
    }
}

function Get-DirectorySizeBytes {
    param([string]$Path)
    $sum = (Get-ChildItem -LiteralPath $Path -Recurse -File -Force -ErrorAction SilentlyContinue | Measure-Object -Property Length -Sum).Sum
    if ($null -eq $sum) {
        return 0
    }
    return [int64]$sum
}

function Format-MB {
    param([int64]$Bytes)
    return "{0:n2}" -f ($Bytes / 1MB)
}

$RepoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$RunDirFull = Resolve-RunDir -Path $RunDir -RepoRoot $RepoRoot
Assert-SafeRunDir -Path $RunDirFull -RepoRoot $RepoRoot

$KeepShortsNames = @($KeepShortsDir -split "," | ForEach-Object { (Split-Path -Leaf $_.Trim()) } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
if ($KeepShortsNames.Count -eq 0) {
    Fail "KeepShortsDir must name at least one directory."
}

$candidates = @(Get-ChildItem -LiteralPath $RunDirFull -Directory -Force |
    Where-Object {
        if ($_.Name -notlike "shorts*") {
            return $false
        }
        foreach ($keep in $KeepShortsNames) {
            if ($_.Name.Equals($keep, [System.StringComparison]::OrdinalIgnoreCase)) {
                return $false
            }
        }
        return $true
    })

$rows = @()
$totalBytes = [int64]0
foreach ($candidate in $candidates) {
    if (-not (Test-PathInside -Child $candidate.FullName -Parent $RunDirFull)) {
        Fail "Refusing unexpected target outside RunDir: $($candidate.FullName)"
    }
    if (-not $candidate.Name.StartsWith("shorts", [System.StringComparison]::OrdinalIgnoreCase)) {
        Fail "Refusing non-shorts target: $($candidate.FullName)"
    }
    $bytes = Get-DirectorySizeBytes -Path $candidate.FullName
    $totalBytes += $bytes
    $rows += [pscustomobject]@{
        Action = if ($Apply) { "delete" } else { "preview" }
        MB = Format-MB -Bytes $bytes
        Path = $candidate.FullName
    }
}

Write-Host "Repo root       : $RepoRoot"
Write-Host "Run dir         : $RunDirFull"
Write-Host "Keep shorts dir : $($KeepShortsNames -join ', ')"
Write-Host "Mode            : $(if ($Apply) { 'APPLY' } else { 'PREVIEW' })"
Write-Host ""
Write-Host "Preserved paths:"
Write-Host "  $(Join-Path $RunDirFull 'recording')"
foreach ($keep in $KeepShortsNames) {
    Write-Host "  $(Join-Path $RunDirFull $keep)"
}
Write-Host "  root files under $RunDirFull"
Write-Host ""

if ($rows.Count -eq 0) {
    Write-Host "No cleanup targets found."
    exit 0
}

Write-Host "Cleanup targets:"
$rows | Format-Table -AutoSize
Write-Host ("Total reclaimable: {0} MB" -f (Format-MB -Bytes $totalBytes))

if (-not $Apply) {
    Write-Host ""
    Write-Host "Preview only. Re-run with -Apply to delete these directories."
    exit 0
}

Write-Host ""
Write-Host "Deleting verified cleanup targets..."
foreach ($candidate in $candidates) {
    Remove-Item -LiteralPath $candidate.FullName -Recurse -Force
    Write-Host "Deleted: $($candidate.FullName)"
}
Write-Host ("Done. Reclaimed approximately {0} MB." -f (Format-MB -Bytes $totalBytes))
