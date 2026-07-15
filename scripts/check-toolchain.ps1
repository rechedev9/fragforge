param(
    [string]$HlaeExe = $env:ZV_HLAE_PATH,
    [string]$Cs2Exe = $env:ZV_CS2_PATH,
    [string]$FfmpegExe = $env:ZV_FFMPEG_PATH,
    [switch]$StrictCapture,
    [switch]$CheckNpxPackages
)

$ErrorActionPreference = "Stop"

$results = New-Object System.Collections.Generic.List[object]

function Add-Check {
    param(
        [string]$Name,
        [bool]$Ok,
        [string]$Detail,
        [bool]$Required = $true
    )

    $results.Add([pscustomobject]@{
        Tool = $Name
        OK = $Ok
        Required = $Required
        Detail = $Detail
    })
}
function Resolve-CommandPath {
    param([string]$Name)

    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if ($null -eq $cmd) {
        return $null
    }
    return $cmd.Source
}

function Test-ExecutablePath {
    param([string]$Path)

    return -not [string]::IsNullOrWhiteSpace($Path) -and (Test-Path -LiteralPath $Path -PathType Leaf)
}

$go = Resolve-CommandPath "go"
Add-Check "Go" ($null -ne $go) $(if ($go) { $go } else { "go not found in PATH" })

$docker = Resolve-CommandPath "docker"
Add-Check "Docker" ($null -ne $docker) $(if ($docker) { $docker } else { "docker not found in PATH" }) $false

$node = Resolve-CommandPath "node"
Add-Check "Node.js" ($null -ne $node) $(if ($node) { $node } else { "node not found in PATH" }) $false

$npx = Resolve-CommandPath "npx"
Add-Check "npx" ($null -ne $npx) $(if ($npx) { $npx } else { "npx not found in PATH" }) $false

$python = Resolve-CommandPath "python"
if ($null -eq $python) {
    $python = Resolve-CommandPath "py"
}
Add-Check "Python" ($null -ne $python) $(if ($python) { $python } else { "python/py not found in PATH" }) $false

if ([string]::IsNullOrWhiteSpace($FfmpegExe)) {
    $FfmpegExe = Resolve-CommandPath "ffmpeg"
}
$ffmpegOk = Test-ExecutablePath $FfmpegExe
Add-Check "FFmpeg" $ffmpegOk $(if ($ffmpegOk) { $FfmpegExe } else { "ffmpeg not found; set ZV_FFMPEG_PATH or PATH" })

$ffprobe = Resolve-CommandPath "ffprobe"
Add-Check "ffprobe" ($null -ne $ffprobe) $(if ($ffprobe) { $ffprobe } else { "ffprobe not found in PATH" })

$captureRequired = [bool]$StrictCapture
$hlaeOk = Test-ExecutablePath $HlaeExe
Add-Check "HLAE" $hlaeOk $(if ($hlaeOk) { $HlaeExe } else { "set ZV_HLAE_PATH or pass -HlaeExe" }) $captureRequired

if ($hlaeOk) {
    $hlaeDir = Split-Path -Parent $HlaeExe
    $hookCandidates = @(
        (Join-Path $hlaeDir "AfxHookSource2.dll"),
        (Join-Path $hlaeDir "x64\AfxHookSource2.dll")
    )
    $hook = $hookCandidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
    Add-Check "HLAE Source 2 hook" ($null -ne $hook) $(if ($hook) { $hook } else { "AfxHookSource2.dll not found next to HLAE.exe or under x64\" }) $captureRequired

    $hlaeFfmpegExe = Join-Path $hlaeDir "ffmpeg\bin\ffmpeg.exe"
    $hlaeFfmpegIni = Join-Path $hlaeDir "ffmpeg\ffmpeg.ini"
    $hlaeFfmpegOk = (Test-Path -LiteralPath $hlaeFfmpegExe -PathType Leaf) -or (Test-Path -LiteralPath $hlaeFfmpegIni -PathType Leaf)
    Add-Check "HLAE FFmpeg config" $hlaeFfmpegOk $(if ($hlaeFfmpegOk) { "configured under $hlaeDir\ffmpeg" } else { "create $hlaeFfmpegIni or install ffmpeg under ffmpeg\bin" }) $captureRequired
}

$cs2Ok = Test-ExecutablePath $Cs2Exe
Add-Check "CS2" $cs2Ok $(if ($cs2Ok) { $Cs2Exe } else { "set ZV_CS2_PATH or pass -Cs2Exe" }) $captureRequired

if ($CheckNpxPackages) {
    if ($null -eq $npx) {
        Add-Check "hyperframes CLI" $false "npx unavailable" $false
        Add-Check "opensrc CLI" $false "npx unavailable" $false
    } else {
        $hyperframesOutput = & npx --yes hyperframes@0.6.40 --version 2>&1
        Add-Check "hyperframes CLI" ($LASTEXITCODE -eq 0) (($hyperframesOutput | Select-Object -First 1) -join "") $false

        $opensrcOutput = & npx --yes opensrc@0.7.3 --version 2>&1
        Add-Check "opensrc CLI" ($LASTEXITCODE -eq 0) (($opensrcOutput | Select-Object -First 1) -join "") $false
    }
}

$results | Format-Table -AutoSize

$failedRequired = $results | Where-Object { $_.Required -and -not $_.OK }
if ($failedRequired.Count -gt 0) {
    Write-Error "Missing required toolchain checks: $($failedRequired.Tool -join ', ')"
    exit 1
}
