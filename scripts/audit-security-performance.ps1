param(
    [switch]$Race
)

$ErrorActionPreference = "Stop"

function Add-KnownCompilerPaths {
    $candidates = @(
        (Join-Path $env:USERPROFILE "scoop\apps\gcc\current\bin"),
        (Join-Path $env:USERPROFILE "scoop\shims"),
        "C:\msys64\ucrt64\bin",
        "C:\msys64\mingw64\bin",
        "C:\Program Files\LLVM\bin"
    )
    $parts = @($env:PATH -split ";" | Where-Object { $_ })
    foreach ($candidate in $candidates) {
        if ((Test-Path -LiteralPath $candidate -PathType Container) -and (($parts | Where-Object { $_ -ieq $candidate }).Count -eq 0)) {
            $parts = @($candidate) + $parts
        }
    }
    $env:PATH = $parts -join ";"
}

function Invoke-Step {
    param(
        [string]$Name,
        [scriptblock]$Block
    )

    Write-Host ""
    Write-Host "==> $Name"
    & $Block
}

Add-KnownCompilerPaths

Invoke-Step "fix loop" {
    & .\scripts\fix-loop.ps1
    if ($LASTEXITCODE -ne 0) {
        throw "fix loop failed"
    }
}

Invoke-Step "gosec" {
    & go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 ./...
    if ($LASTEXITCODE -ne 0) {
        throw "gosec failed"
    }
}

Invoke-Step "govulncheck" {
    & go run golang.org/x/vuln/cmd/govulncheck@v1.4.0 ./...
    if ($LASTEXITCODE -ne 0) {
        throw "govulncheck failed"
    }
}

Invoke-Step "benchmark compile" {
    & go test ./... -run=^$ -bench=. -benchmem
    if ($LASTEXITCODE -ne 0) {
        throw "benchmark compile failed"
    }
}

Invoke-Step "diff whitespace" {
    & git diff --check
    if ($LASTEXITCODE -ne 0) {
        throw "git diff --check failed"
    }
}

if ($Race) {
    $gcc = Get-Command gcc -ErrorAction SilentlyContinue
    if ($null -eq $gcc) {
        Write-Warning "Skipping race detector: CGO race builds require gcc in PATH on Windows."
    } else {
        Invoke-Step "race detector" {
            $env:CGO_ENABLED = "1"
            & go test -race ./...
            if ($LASTEXITCODE -ne 0) {
                throw "race detector failed"
            }
        }
    }
}

Write-Host ""
Write-Host "Security/performance audit score: 5/5"
