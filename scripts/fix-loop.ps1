param(
    [int]$MaxIterations = 3,
    [switch]$NoFormat,
    [switch]$NoBuild,
    [switch]$NoMediaCheck,
    [switch]$Toolchain
)

$ErrorActionPreference = "Stop"

function Invoke-Step {
    param(
        [string]$Name,
        [scriptblock]$Block
    )

    Write-Host ""
    Write-Host "==> $Name"
    & $Block
}

function Get-GoFilesNeedingFormat {
    $files = & gofmt -l .
    if ($LASTEXITCODE -ne 0) {
        throw "gofmt -l failed"
    }
    return @($files | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}

function Test-NoGeneratedMedia {
    $patterns = @("*.mp4", "*.mov", "*.webm", "*.avi", "*.mkv", "*.dem")
    $found = foreach ($pattern in $patterns) {
        Get-ChildItem -Path . -Recurse -File -Filter $pattern -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -notmatch "\\.git\\" }
    }

    $found = @($found)
    if ($found.Count -gt 0) {
        $found | Select-Object FullName, Length | Format-Table -AutoSize
        throw "Generated media/demo files found in the repo tree"
    }
}

if ($MaxIterations -lt 1) {
    throw "-MaxIterations must be >= 1"
}

for ($i = 1; $i -le $MaxIterations; $i++) {
    Write-Host ""
    Write-Host "===== Fix loop iteration $i/$MaxIterations ====="
    $changed = $false

    if (-not $NoFormat) {
        Invoke-Step "gofmt check" {
            $files = Get-GoFilesNeedingFormat
            if ($files.Count -gt 0) {
                Write-Host "Formatting:"
                $files | ForEach-Object { Write-Host "  $_" }
                & gofmt -w @files
                if ($LASTEXITCODE -ne 0) {
                    throw "gofmt -w failed"
                }
                $script:changed = $true
            } else {
                Write-Host "No formatting changes needed."
            }
        }
    }

    Invoke-Step "go vet ./..." {
        & go vet ./...
        if ($LASTEXITCODE -ne 0) {
            throw "go vet failed"
        }
    }

    Invoke-Step "go test ./... -count=1" {
        & go test ./... -count=1
        if ($LASTEXITCODE -ne 0) {
            throw "go test failed"
        }
    }

    Invoke-Step "zv check" {
        & go run ./cmd/zv check
        if ($LASTEXITCODE -ne 0) {
            throw "zv check failed"
        }
    }

    if (-not $NoBuild) {
        Invoke-Step "scripts/build.ps1" {
            & .\scripts\build.ps1
            if ($LASTEXITCODE -ne 0) {
                throw "build script failed"
            }
        }
    }

    if (-not $NoMediaCheck) {
        Invoke-Step "generated media check" {
            Test-NoGeneratedMedia
            Write-Host "No generated media/demo files found."
        }
    }

    if ($Toolchain) {
        Invoke-Step "toolchain check" {
            & .\scripts\check-toolchain.ps1
            if ($LASTEXITCODE -ne 0) {
                throw "toolchain check failed"
            }
        }
    }

    if (-not $changed) {
        Write-Host ""
        Write-Host "Fix loop converged: no automatic fixes were needed in this iteration."
        exit 0
    }
}

Write-Error "Fix loop reached MaxIterations after applying changes. Re-run to verify convergence."
exit 1
