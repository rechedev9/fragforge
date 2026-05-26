$ErrorActionPreference = "Stop"

$commands = @(
    "zv",
    "zv-parser",
    "zv-demo-players",
    "zv-orchestrator",
    "zv-recorder",
    "zv-composer",
    "zv-editor",
    "zv-analysis-viewer",
    "zv-pipeline",
    "zv-tactical-data"
)

$binDir = Join-Path (Resolve-Path ".").Path "bin"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

foreach ($name in $commands) {
    $out = Join-Path $binDir "$name.exe"
    $pkg = "./cmd/$name"
    Write-Host "go build -o $out $pkg"
    & go build -o $out $pkg
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed for $pkg"
    }
}
