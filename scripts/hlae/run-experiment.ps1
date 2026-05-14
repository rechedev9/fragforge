<#
.SYNOPSIS
    Runs a single HLAE prototype experiment by name.

.DESCRIPTION
    Launches CS2 through HLAE with the experiment's .mirv script preloaded,
    waits for the game to exit, and reports the output directory.

.PARAMETER Experiment
    Experiment id (one of e1, e2, e3, e4).

.PARAMETER Demo
    Absolute path to the .dem file (expected: lavked-vs-tnc-m2-nuke.dem).

.PARAMETER HlaeExe
    Absolute path to HLAE.exe.

.PARAMETER Cs2Exe
    Absolute path to cs2.exe. Defaults to the Steam install path.

.PARAMETER OutDir
    Where HLAE writes recordings and frames.
    Defaults to "$env:TEMP\zv-hlae\<experiment>".

.EXAMPLE
    .\run-experiment.ps1 -Experiment e1 `
        -Demo "C:\demos\lavked-vs-tnc-m2-nuke.dem" `
        -HlaeExe "C:\HLAE\HLAE.exe"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('e1', 'e2', 'e3', 'e4')]
    [string]$Experiment,

    [Parameter(Mandatory = $true)]
    [string]$Demo,

    [Parameter(Mandatory = $true)]
    [string]$HlaeExe,

    [string]$Cs2Exe = "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe",

    [string]$OutDir
)

$ErrorActionPreference = 'Stop'

# Resolve script directory to find the .mirv file.
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$MirvMap = @{
    'e1' = 'e1-seek-accuracy.mirv'
    'e2' = 'e2-multi-segment.mirv'
    'e3' = 'e3-output-format.mirv'
    'e4' = 'e4-host-timescale.mirv'
}
$MirvPath = Join-Path $ScriptDir $MirvMap[$Experiment]

# Validate inputs.
if (-not (Test-Path $Demo))     { throw "Demo not found: $Demo" }
if (-not (Test-Path $HlaeExe))  { throw "HLAE not found: $HlaeExe" }
if (-not (Test-Path $Cs2Exe))   { throw "CS2 not found: $Cs2Exe" }
if (-not (Test-Path $MirvPath)) { throw "Mirv script not found: $MirvPath" }

if (-not $OutDir) {
    $OutDir = Join-Path $env:TEMP "zv-hlae\$Experiment"
}
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

Write-Host "Experiment : $Experiment"
Write-Host "Demo       : $Demo"
Write-Host "Mirv script: $MirvPath"
Write-Host "Output dir : $OutDir"
Write-Host ""

# Build HLAE arguments.
$HookDll = Join-Path (Split-Path -Parent $HlaeExe) 'AfxHookSource2.dll'
if (-not (Test-Path $HookDll)) {
    throw "AfxHookSource2.dll not found next to HLAE.exe at $HookDll"
}

$CmdLine = "+playdemo `"$Demo`" +mirv_script_load `"$MirvPath`""

$Args = @(
    '-csgoLauncher',
    '-noGui',
    '-autoStart',
    '-hookDllPath',  "`"$HookDll`"",
    '-programPath',  "`"$Cs2Exe`"",
    '-cmdLine',      "`"$CmdLine`""
)

# Track wall-clock time (needed for E4).
$sw = [System.Diagnostics.Stopwatch]::StartNew()
Write-Host "Launching HLAE..."
$proc = Start-Process -FilePath $HlaeExe -ArgumentList $Args -Wait -PassThru -NoNewWindow
$sw.Stop()

Write-Host ""
Write-Host "HLAE exited with code $($proc.ExitCode)"
Write-Host "Wall-clock duration: $([math]::Round($sw.Elapsed.TotalSeconds, 2)) s"
Write-Host ""
Write-Host "Output directory contents:"
Get-ChildItem -Path $OutDir -Recurse | Format-Table FullName, Length
