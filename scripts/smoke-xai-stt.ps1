param(
    [Parameter(Mandatory = $true)]
    [string]$MediaPath,

    [string]$Language = "auto",

    [string]$ASSPath = "",

    [string]$ExpectedText = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($env:XAI_API_KEY)) {
    throw "XAI_API_KEY is not set in this PowerShell session"
}

$resolvedMedia = (Resolve-Path -LiteralPath $MediaPath).Path
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$previousMedia = $env:ZV_XAI_STT_MEDIA
$previousLanguage = $env:ZV_XAI_STT_LANGUAGE
$previousASS = $env:ZV_XAI_STT_ASS
$previousExpected = $env:ZV_XAI_STT_EXPECTED

try {
    $env:ZV_XAI_STT_MEDIA = $resolvedMedia
    $env:ZV_XAI_STT_LANGUAGE = if ($Language -eq "auto") { "" } else { $Language }
    $env:ZV_XAI_STT_ASS = if ($ASSPath) {
        [System.IO.Path]::GetFullPath($ASSPath)
    } else {
        ""
    }
    $env:ZV_XAI_STT_EXPECTED = $ExpectedText

    Push-Location $repoRoot
    try {
        & go test ./internal/captions -run '^TestXAITranscriberLive$' -count=1 -v
        if ($LASTEXITCODE -ne 0) {
            throw "xAI speech-to-text smoke test failed"
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    $env:ZV_XAI_STT_MEDIA = $previousMedia
    $env:ZV_XAI_STT_LANGUAGE = $previousLanguage
    $env:ZV_XAI_STT_ASS = $previousASS
    $env:ZV_XAI_STT_EXPECTED = $previousExpected
}
