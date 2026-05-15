<#
.SYNOPSIS
    Runs a single HLAE prototype experiment by name.

.DESCRIPTION
    Launches CS2 through HLAE with the experiment's JavaScript mirv-script
    preloaded, waits for CS2 to exit, and reports the output directory.

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

.PARAMETER TimeoutSeconds
    Maximum time to wait for CS2 after HLAE dispatches the launch.

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

    [string]$OutDir,

    [int]$TimeoutSeconds = 900,

    [int]$Width = 1920,

    [int]$Height = 1080
)

$ErrorActionPreference = 'Stop'

# Resolve script directory to find the HLAE JavaScript file.
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ScriptMap = @{
    'e1' = 'e1-seek-accuracy.js'
    'e2' = 'e2-multi-segment.js'
    'e3' = 'e3-output-format.js'
    'e4' = 'e4-host-timescale.js'
}
$ScriptPath = Join-Path $ScriptDir $ScriptMap[$Experiment]

# Validate inputs.
if (-not (Test-Path $Demo))     { throw "Demo not found: $Demo" }
if (-not (Test-Path $HlaeExe))  { throw "HLAE not found: $HlaeExe" }
if (-not (Test-Path $Cs2Exe))   { throw "CS2 not found: $Cs2Exe" }
if (-not (Test-Path $ScriptPath)) { throw "HLAE JS script not found: $ScriptPath" }

if (-not $OutDir) {
    $OutDir = Join-Path $env:TEMP "zv-hlae\$Experiment"
}
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$OutDir = (Resolve-Path $OutDir).Path
$Demo = (Resolve-Path $Demo).Path
$HlaeExe = (Resolve-Path $HlaeExe).Path
$Cs2Exe = (Resolve-Path $Cs2Exe).Path

$RenderedScript = Join-Path $OutDir "$Experiment.rendered.js"
$JsOutDir = $OutDir.Replace('\', '/').Replace('"', '\"')
$RenderedContent = (Get-Content -Raw $ScriptPath).Replace('__ZV_OUT_DIR__', $JsOutDir)
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($RenderedScript, $RenderedContent, $Utf8NoBom)

Write-Host "Experiment : $Experiment"
Write-Host "Demo       : $Demo"
Write-Host "JS script  : $RenderedScript"
Write-Host "Output dir : $OutDir"
Write-Host ""

# Locate the HLAE Source-2 hook DLL. Older HLAE releases shipped it next to
# HLAE.exe; HLAE 2.x ships it in the x64\ subfolder. Accept either layout.
$HlaeDir = Split-Path -Parent $HlaeExe
$HookDll = Join-Path $HlaeDir 'AfxHookSource2.dll'
if (-not (Test-Path $HookDll)) {
    $HookDll = Join-Path $HlaeDir 'x64\AfxHookSource2.dll'
    if (-not (Test-Path $HookDll)) {
        throw "AfxHookSource2.dll not found next to HLAE.exe or under '$HlaeDir\x64\'."
    }
}

$HlaeFfmpegDir = Join-Path $HlaeDir 'ffmpeg'
$HlaeFfmpegExe = Join-Path $HlaeFfmpegDir 'bin\ffmpeg.exe'
$HlaeFfmpegIni = Join-Path $HlaeFfmpegDir 'ffmpeg.ini'
if ((Test-Path $HlaeFfmpegDir) -and -not (Test-Path $HlaeFfmpegExe) -and -not (Test-Path $HlaeFfmpegIni)) {
    $SystemFfmpeg = Get-Command ffmpeg -ErrorAction SilentlyContinue
    if (-not $SystemFfmpeg) {
        throw "HLAE FFmpeg config is missing and ffmpeg.exe is not in PATH. Install FFmpeg or create '$HlaeFfmpegIni'."
    }
    $FfmpegIniContent = "[Ffmpeg]`r`nPath=$($SystemFfmpeg.Source)`r`n"
    [System.IO.File]::WriteAllText($HlaeFfmpegIni, $FfmpegIniContent, $Utf8NoBom)
}

# Build the full command line as a single string with Windows-canonical
# quoting (CommandLineToArgvW rules: inside a quoted token, \" is a literal
# double quote). Native-exe launch via Start-Process -ArgumentList @() does
# NOT escape inner quotes reliably on PowerShell 5.1, so we assemble the
# string ourselves and feed it to ProcessStartInfo.
# -insecure is required by AfxHookSource2 — without it the hook shows
# "Please add -insecure to launch options, AfxHookSource2 will refuse
# to work without it!" and bails.
$existingCs2 = Get-Process -Name "cs2" -ErrorAction SilentlyContinue | Where-Object {
    try { $_.Path -eq $Cs2Exe } catch { $false }
}
if ($existingCs2) {
    throw "CS2 is already running from '$Cs2Exe'. Close it before running the experiment."
}

$Cs2CmdLine        = '-insecure -condebug -w ' + $Width + ' -h ' + $Height + ' +playdemo "' + $Demo + '" +mirv_script_load "' + $RenderedScript + '"'
$Cs2CmdLineEscaped = $Cs2CmdLine -replace '"', '\"'

# Use -customLoader (generic DLL-injection launcher) instead of
# -csgoLauncher: the latter is CS:GO-specific and fails on cs2.exe with
# HLAE error 2002 / Win32Error 123 (ERROR_INVALID_NAME). The HLAE wiki
# recommends -customLoader for Source-2 games.
$HlaeArgString =
    '-customLoader -noGui -autoStart' +
    ' -hookDllPath "' + $HookDll + '"' +
    ' -programPath "' + $Cs2Exe + '"' +
    ' -cmdLine "' + $Cs2CmdLineEscaped + '"'

Write-Host "Launching HLAE..."
Write-Host "Args: $HlaeArgString"

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName        = $HlaeExe
$psi.Arguments       = $HlaeArgString
$psi.UseShellExecute = $false

$sw = [System.Diagnostics.Stopwatch]::StartNew()
$proc = [System.Diagnostics.Process]::Start($psi)
$proc.WaitForExit()

Write-Host ""
Write-Host "HLAE exited with code $($proc.ExitCode)"

$deadline = (Get-Date).AddSeconds(60)
$cs2 = $null
while ((Get-Date) -lt $deadline -and -not $cs2) {
    Start-Sleep -Milliseconds 500
    $cs2 = Get-Process -Name "cs2" -ErrorAction SilentlyContinue | Where-Object {
        try { $_.Path -eq $Cs2Exe } catch { $false }
    } | Select-Object -First 1
}

if (-not $cs2) {
    $sw.Stop()
    throw "HLAE exited, but no matching cs2.exe process appeared within 60 seconds."
}

Write-Host "CS2 pid   : $($cs2.Id)"
Write-Host "Waiting for CS2 to finish (timeout ${TimeoutSeconds}s)..."

$exited = $cs2.WaitForExit($TimeoutSeconds * 1000)
$sw.Stop()
if (-not $exited) {
    Stop-Process -Id $cs2.Id -Force -ErrorAction SilentlyContinue
    throw "Timed out after ${TimeoutSeconds}s waiting for CS2; killed pid $($cs2.Id)."
}

Write-Host "CS2 exited."
Write-Host "Wall-clock duration: $([math]::Round($sw.Elapsed.TotalSeconds, 2)) s"
Write-Host ""
Write-Host "Output directory contents:"
Get-ChildItem -Path $OutDir -Recurse -File | Format-Table FullName, Length -AutoSize
