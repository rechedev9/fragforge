param(
    [Parameter(Mandatory = $true)]
    [string]$MediaPath,

    [string]$ASSPath = "",

    [string]$ExpectedSpanish = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($env:XAI_API_KEY)) {
    throw "XAI_API_KEY is not set in this PowerShell session"
}

$resolvedMedia = (Resolve-Path -LiteralPath $MediaPath).Path
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$previousMedia = $env:ZV_XAI_SPANISH_MEDIA
$previousASS = $env:ZV_XAI_SPANISH_ASS
$previousExpected = $env:ZV_XAI_SPANISH_EXPECTED

try {
    $env:ZV_XAI_SPANISH_MEDIA = $resolvedMedia
    $env:ZV_XAI_SPANISH_ASS = if ($ASSPath) {
        [System.IO.Path]::GetFullPath($ASSPath)
    } else {
        ""
    }
    $env:ZV_XAI_SPANISH_EXPECTED = $ExpectedSpanish

    Push-Location $repoRoot
    try {
        & go test ./internal/captions -run '^TestXAISpanishCaptionsLive$' -count=1 -v
        if ($LASTEXITCODE -ne 0) {
            throw "xAI Spanish caption smoke test failed"
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    $env:ZV_XAI_SPANISH_MEDIA = $previousMedia
    $env:ZV_XAI_SPANISH_ASS = $previousASS
    $env:ZV_XAI_SPANISH_EXPECTED = $previousExpected
}
