# FragForge Local Studio
#
# One command to run the whole product on the user's own Windows + GPU PC: the
# orchestrator (parse + HLAE/CS2 capture + render) and the web UI, wired so the
# browser flow (upload -> pick player -> pick kills -> create reel) actually
# drives local capture. Everything runs on this machine.
#
# What it does:
#   1. Starts `zv serve` with an on-disk SQLite job database and inline queue
#      (no external database or queue service). The orchestrator auto-detects
#      HLAE, CS2, and zv-recorder on startup, so capture works without setting
#      any tool-path env vars.
#   2. Starts the Next.js web UI, whose /api/demos/* routes proxy the full
#      pipeline to the local orchestrator.
#   3. Opens the browser at the upload page.
#
# Ctrl+C stops the web UI and then the orchestrator.
#
# Prerequisites:
#   - Build the binaries first:  .\scripts\build.ps1   (produces .\bin\zv.exe)
#   - Node.js + pnpm 11.9.0. If node_modules is missing, the script installs
#     exactly the dependency graph in pnpm-lock.yaml.
#   - CS2 + the latest official HLAE installed under C:\HLAE-<version>\HLAE.exe. Capture needs them;
#     without them the app still runs the analyze flow and the Capture card tells
#     you what is missing.

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$binZv = Join-Path $repoRoot "bin\zv.exe"
$webDir = Join-Path $repoRoot "web"
$dataDir = Join-Path $repoRoot "data"

# Loopback only: the web server proxies to the orchestrator over localhost, and
# the browser only ever talks to the web server. A loopback bind needs no token.
$orchestratorUrl = "http://127.0.0.1:8080"
$webUrl = "http://localhost:3000"

if (-not (Test-Path $binZv)) {
    throw "missing $binZv - build the binaries first with .\scripts\build.ps1"
}

if (-not (Test-Path (Join-Path $webDir "node_modules"))) {
    Write-Host "[local-studio] installing web dependencies (first run)..."
    Push-Location $webDir
    try { & pnpm install --frozen-lockfile; if ($LASTEXITCODE -ne 0) { throw "pnpm install failed" } }
    finally { Pop-Location }
}

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

# Provision the music library into <repo>\data\music, the directory the
# orchestrator serves when ZV_MUSIC_DIR is unset. Best-effort: a machine that is
# offline (or already provisioned) still boots Local Studio, just with whatever
# tracks are present. sha256 mismatches are discarded so a truncated download
# never poisons the library.
$musicDir = Join-Path $dataDir "music"
$catalogPath = Join-Path $musicDir "catalog.json"
if (Test-Path $catalogPath) {
    Write-Host "[local-studio] provisioning music library (best-effort)..."
    try {
        $catalog = Get-Content $catalogPath -Raw | ConvertFrom-Json
        foreach ($track in $catalog.tracks) {
            if (-not $track.downloadUrl -or -not $track.id -or -not $track.ext -or -not $track.sha256) { continue }
            $trackPath = Join-Path $musicDir "$($track.id).$($track.ext)"
            if (Test-Path $trackPath) { continue }
            try {
                Invoke-WebRequest -Uri $track.downloadUrl -OutFile $trackPath -UseBasicParsing -TimeoutSec 60
                $digest = (Get-FileHash -Path $trackPath -Algorithm SHA256).Hash.ToLowerInvariant()
                if ($digest -ne $track.sha256.ToLowerInvariant()) {
                    Remove-Item $trackPath -Force
                    Write-Host "[local-studio]   discarded $($track.id) (sha256 mismatch)"
                } else {
                    Write-Host "[local-studio]   downloaded $($track.id).$($track.ext)"
                }
            } catch {
                if (Test-Path $trackPath) { Remove-Item $trackPath -Force -ErrorAction SilentlyContinue }
                Write-Host "[local-studio]   skipped $($track.id): $($_.Exception.Message)"
            }
        }
    } catch {
        Write-Host "[local-studio] music provisioning skipped: $($_.Exception.Message)"
    }
}

Write-Host "[local-studio] starting orchestrator (SQLite jobs, capture auto-detected)..."
# Set in the session so the orchestrator child inherits them (works on both
# Windows PowerShell 5.1 and PowerShell 7; Start-Process -Environment is 7.4+).
# sqlite = on-disk job repo (<dataDir>/jobs.db) + inline queue, with no external
# services, so jobs survive a restart.
$env:ZV_DATABASE_URL = "sqlite"
$env:ZV_DATA_DIR = $dataDir
$orchestrator = Start-Process -FilePath $binZv -ArgumentList "serve" -PassThru -NoNewWindow

try {
    # Wait for the orchestrator to answer /healthz before starting the web UI, so
    # the first upload does not race a not-yet-listening backend.
    Write-Host "[local-studio] waiting for orchestrator health..."
    $healthy = $false
    for ($i = 0; $i -lt 40; $i++) {
        if ($orchestrator.HasExited) { throw "orchestrator exited early (code $($orchestrator.ExitCode))" }
        try {
            $res = Invoke-WebRequest -Uri "$orchestratorUrl/healthz" -UseBasicParsing -TimeoutSec 2
            if ($res.StatusCode -eq 200) { $healthy = $true; break }
        } catch {
            Start-Sleep -Milliseconds 500
        }
    }
    if (-not $healthy) { throw "orchestrator did not become healthy at $orchestratorUrl" }
    Write-Host "[local-studio] orchestrator healthy at $orchestratorUrl"

    # The /api/demos/* routes proxy the whole pipeline to the local
    # orchestrator; ORCHESTRATOR_URL is read server-side by the Next.js server.
    $env:ORCHESTRATOR_URL = $orchestratorUrl

    Write-Host "[local-studio] opening $webUrl/upload"
    Start-Process $webUrl/upload

    Write-Host "[local-studio] starting web UI (Ctrl+C to stop everything)..."
    Push-Location $webDir
    try { & pnpm run dev }
    finally { Pop-Location }
}
finally {
    if ($orchestrator -and -not $orchestrator.HasExited) {
        Write-Host "[local-studio] stopping orchestrator..."
        Stop-Process -Id $orchestrator.Id -Force -ErrorAction SilentlyContinue
    }
}
