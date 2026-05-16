param(
    [string]$Demo = "testdata\lavked-vs-tnc-m2-nuke.dem",
    [string]$TargetSteamID = "76561198148986856",
    [string]$BaseUrl = "",
    [int]$TimeoutSeconds = 1800,
    [string]$OutDir = "",
    [switch]$SkipInfra,
    [switch]$RequireFFprobe
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Fail {
    param([string]$Message)
    throw $Message
}

function Require-Env {
    param([string]$Name)
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrWhiteSpace($value)) {
        Fail "Missing required environment variable $Name."
    }
    return $value
}

function Invoke-Curl {
    param([string[]]$Arguments, [string]$Description)
    $output = & curl.exe @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        Fail "$Description failed: $($output -join "`n")"
    }
    return ($output -join "`n")
}

function Get-Job {
    param([string]$JobID)
    $raw = Invoke-Curl -Description "GET /api/jobs/$JobID" -Arguments @(
        "-fsS",
        "$BaseUrl/api/jobs/$JobID"
    )
    return ($raw | ConvertFrom-Json)
}

function Wait-JobStatus {
    param(
        [string]$JobID,
        [string[]]$Desired,
        [string]$Phase
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    $lastStatus = ""

    while ((Get-Date) -lt $deadline) {
        $job = Get-Job -JobID $JobID
        $status = [string]$job.status
        if ($status -ne $lastStatus) {
            Write-Host ("{0}: status={1}" -f $Phase, $status)
            $lastStatus = $status
        }
        if ($status -eq "failed") {
            Fail "$Phase failed: $($job.failure_reason)"
        }
        if ($Desired -contains $status) {
            $stopwatch.Stop()
            return [pscustomobject]@{
                Job = $job
                Seconds = [math]::Round($stopwatch.Elapsed.TotalSeconds, 2)
            }
        }
        Start-Sleep -Seconds 2
    }

    Fail "Timed out waiting for $Phase to reach $($Desired -join "/") after $TimeoutSeconds seconds."
}

function Invoke-JobPost {
    param([string]$JobID, [string]$Action)
    [void](Invoke-Curl -Description "POST /api/jobs/$JobID/$Action" -Arguments @(
        "-fsS",
        "-X", "POST",
        "$BaseUrl/api/jobs/$JobID/$Action"
    ))
}

function Convert-Ratio {
    param([string]$Ratio)
    $parts = $Ratio -split "/"
    if ($parts.Count -ne 2 -or [double]$parts[1] -eq 0) {
        return 0.0
    }
    return ([double]$parts[0] / [double]$parts[1])
}

function Test-FinalWithFFprobe {
    param([string]$Path)

    $ffprobe = [Environment]::GetEnvironmentVariable("ZV_FFPROBE_PATH")
    if ([string]::IsNullOrWhiteSpace($ffprobe)) {
        $cmd = Get-Command ffprobe -ErrorAction SilentlyContinue
        if ($cmd) {
            $ffprobe = $cmd.Source
        }
    }
    if ([string]::IsNullOrWhiteSpace($ffprobe) -or -not (Test-Path -LiteralPath $ffprobe)) {
        if ($RequireFFprobe) {
            Fail "ffprobe not found. Set ZV_FFPROBE_PATH or add ffprobe to PATH."
        }
        Write-Warning "ffprobe not found; skipping codec/fps/audio verification."
        return
    }

    $raw = & $ffprobe -v error -print_format json -show_streams $Path 2>&1
    if ($LASTEXITCODE -ne 0) {
        Fail "ffprobe failed: $($raw -join "`n")"
    }
    $probe = (($raw -join "`n") | ConvertFrom-Json)
    $video = @($probe.streams | Where-Object { $_.codec_type -eq "video" } | Select-Object -First 1)
    $audio = @($probe.streams | Where-Object { $_.codec_type -eq "audio" } | Select-Object -First 1)
    if ($video.Count -eq 0) {
        Fail "ffprobe did not find a video stream."
    }
    if ($audio.Count -eq 0) {
        Fail "ffprobe did not find an audio stream."
    }

    $fps = Convert-Ratio -Ratio ([string]$video[0].avg_frame_rate)
    if ($fps -eq 0) {
        $fps = Convert-Ratio -Ratio ([string]$video[0].r_frame_rate)
    }
    if ($video[0].codec_name -ne "h264") {
        Fail "Expected H.264 video, got $($video[0].codec_name)."
    }
    if ([int]$video[0].width -ne 1920 -or [int]$video[0].height -ne 1080) {
        Fail "Expected 1920x1080 video, got $($video[0].width)x$($video[0].height)."
    }
    if ([math]::Abs($fps - 60.0) -gt 0.2) {
        Fail "Expected 60fps video, got $fps."
    }
    Write-Host ("ffprobe: video=h264 {0}x{1} {2:n2}fps audio={3}" -f $video[0].width, $video[0].height, $fps, $audio[0].codec_name)
}

[void](Require-Env "ZV_DATABASE_URL")
[void](Require-Env "ZV_RECORDER_PATH")
[void](Require-Env "ZV_HLAE_PATH")
[void](Require-Env "ZV_CS2_PATH")
[void](Require-Env "ZV_COMPOSER_PATH")

if (-not (Get-Command curl.exe -ErrorAction SilentlyContinue)) {
    Fail "curl.exe is required."
}

if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
    $BaseUrl = [Environment]::GetEnvironmentVariable("ZV_BASE_URL")
    if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
        $BaseUrl = "http://localhost:8080"
    }
}
$BaseUrl = $BaseUrl.TrimEnd("/")

if ([string]::IsNullOrWhiteSpace($OutDir)) {
    $OutDir = Join-Path (Get-Location) "data\smoke"
}
$DemoPath = (Resolve-Path -LiteralPath $Demo).Path
[void](New-Item -ItemType Directory -Force -Path $OutDir)

if (-not $SkipInfra) {
    if ((Test-Path -LiteralPath "docker-compose.yml") -and (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Host "Starting Postgres and Redis with docker compose..."
        $infra = & docker compose up -d postgres redis 2>&1
        if ($LASTEXITCODE -ne 0) {
            Fail "docker compose up failed: $($infra -join "`n")"
        }
    } else {
        Write-Host "Docker compose not available; assuming Postgres and Redis are already running."
    }
}

$probeID = [guid]::NewGuid().ToString()
$probeStatus = & curl.exe -sS -o NUL -w "%{http_code}" "$BaseUrl/api/jobs/$probeID" 2>$null
if ($LASTEXITCODE -ne 0 -or $probeStatus -ne "404") {
    Fail "Orchestrator is not reachable or healthy at $BaseUrl (probe status=$probeStatus). Start bin\zv-orchestrator with the current environment and run migrations first."
}

Write-Host "Uploading demo..."
$uploadWatch = [System.Diagnostics.Stopwatch]::StartNew()
$configJson = ('{{"target_steamid":"{0}"}}' -f $TargetSteamID)
$jobRaw = Invoke-Curl -Description "POST /api/jobs" -Arguments @(
    "-fsS",
    "-X", "POST",
    "$BaseUrl/api/jobs",
    "-F", "demo=@$DemoPath",
    "-F", "config=$configJson"
)
$uploadWatch.Stop()
$job = ($jobRaw | ConvertFrom-Json)
$jobID = [string]$job.id
Write-Host ("job_id={0}" -f $jobID)

$parsed = Wait-JobStatus -JobID $jobID -Desired @("parsed") -Phase "parse"

Invoke-JobPost -JobID $jobID -Action "record"
$recorded = Wait-JobStatus -JobID $jobID -Desired @("recorded") -Phase "record"

Invoke-JobPost -JobID $jobID -Action "record"
Start-Sleep -Seconds 2
$recordRetry = Get-Job -JobID $jobID
if ([string]$recordRetry.status -ne "recorded") {
    $recordedRetry = Wait-JobStatus -JobID $jobID -Desired @("recorded") -Phase "record-retry"
    $recordRetrySeconds = $recordedRetry.Seconds
} else {
    $recordRetrySeconds = 0
}

Invoke-JobPost -JobID $jobID -Action "compose"
$composed = Wait-JobStatus -JobID $jobID -Desired @("composed") -Phase "compose"

Invoke-JobPost -JobID $jobID -Action "compose"
Start-Sleep -Seconds 2
$composeRetry = Get-Job -JobID $jobID
if ([string]$composeRetry.status -ne "composed") {
    $composedRetry = Wait-JobStatus -JobID $jobID -Desired @("composed") -Phase "compose-retry"
    $composeRetrySeconds = $composedRetry.Seconds
} else {
    $composeRetrySeconds = 0
}

$finalPath = Join-Path $OutDir "$jobID-final.mp4"
[void](Invoke-Curl -Description "GET /api/jobs/$jobID/final" -Arguments @(
    "-fsS",
    "-L",
    "-o", $finalPath,
    "$BaseUrl/api/jobs/$jobID/final"
))
$finalSize = (Get-Item -LiteralPath $finalPath).Length
if ($finalSize -le 0) {
    Fail "Downloaded final MP4 is empty: $finalPath"
}

Test-FinalWithFFprobe -Path $finalPath

Write-Host ("upload_seconds={0:n2}" -f $uploadWatch.Elapsed.TotalSeconds)
Write-Host ("parse_seconds={0:n2}" -f $parsed.Seconds)
Write-Host ("record_seconds={0:n2}" -f $recorded.Seconds)
Write-Host ("record_retry_seconds={0:n2}" -f $recordRetrySeconds)
Write-Host ("compose_seconds={0:n2}" -f $composed.Seconds)
Write-Host ("compose_retry_seconds={0:n2}" -f $composeRetrySeconds)
Write-Host ("final_path={0}" -f $finalPath)
Write-Host ("final_size_bytes={0}" -f $finalSize)
