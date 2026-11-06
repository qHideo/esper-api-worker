# Signs a published release and deploys its manifest to the Worker.
#
# CI: triggered by the release workflow with -Tag/-Channel.
# Local dry run:
#   .\ci\release.ps1 -Version 0.1.32 -Payload ..\payload.zip `
#       -AssetUrl https://github.com/owner/repo/releases/download/v0.1.32/Esper-Client.zip `
#       -SeedFile ..\esper-api\keys\seed.b64 -Out dist\ci-test.json -NoDeploy
param(
    [string]$Tag = "",
    [string]$Version = "",
    [string]$Channel = "beta",
    [int64]$Build = 0,
    [string]$Payload = "",
    [string]$AssetUrl = "",
    [string]$SeedFile = "",
    [string]$Out = "public/v1/latest.json",
    [string]$NotesUrl = "https://esper-api.esper-api-worker.workers.dev/v1/news.json",
    [switch]$NoDeploy
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$root = (Get-Location).Path

# The portable Node used for local deploys, when npx is not on PATH.
$portableNode = Join-Path $root "..\tools\node-v26.13.0-win-x64"
if ((Test-Path (Join-Path $portableNode "npx.cmd")) -and
    -not (Get-Command npx -ErrorAction SilentlyContinue)) {
    $env:PATH = "$portableNode;" + $env:PATH
}

if (-not $Version) {
    if ($Tag) { $Version = $Tag -replace '^v', '' }
    elseif ($env:GITHUB_REF_NAME) { $Version = $env:GITHUB_REF_NAME -replace '^v', '' }
}
if (-not $Version) { throw "Provide -Tag, -Version, or run from a tag build." }
if (-not $Tag) { $Tag = "v$Version" }

if ($Build -le 0) {
    if ($env:GITHUB_RUN_NUMBER) { $Build = [int64]$env:GITHUB_RUN_NUMBER }
    else { $Build = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() }
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("esper-release-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force $temp | Out-Null

try {
    if ($Payload) {
        $payloadPath = (Resolve-Path $Payload).Path
        $assetName = Split-Path -Leaf $payloadPath
    } else {
        if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
            throw "gh is unavailable; pass -Payload for a local run."
        }
        & gh release download $Tag --pattern "*.zip" --dir $temp
        if ($LASTEXITCODE -ne 0) { throw "Release $Tag has no .zip asset." }
        $zips = @(Get-ChildItem $temp -Filter "*.zip")
        if ($zips.Count -eq 0) { throw "Release $Tag has no .zip asset." }
        if ($zips.Count -gt 1) {
            Write-Warning "Multiple .zip assets; using $($zips[0].Name)."
        }
        $payloadPath = $zips[0].FullName
        $assetName = $zips[0].Name
    }

    if ($AssetUrl) {
        $artifactUrl = $AssetUrl
    } else {
        if (-not $env:GITHUB_REPOSITORY) {
            throw "GITHUB_REPOSITORY is unset; pass -AssetUrl."
        }
        $artifactUrl = "https://github.com/$($env:GITHUB_REPOSITORY)/releases/download/$Tag/$assetName"
    }
    if (-not $artifactUrl.StartsWith("https://")) {
        throw "Artifact URL must be HTTPS: $artifactUrl"
    }

    if ($SeedFile) {
        $seedPath = (Resolve-Path $SeedFile).Path
    } else {
        if (-not $env:UPDATE_SIGNING_SEED) {
            throw "UPDATE_SIGNING_SEED is unset and no -SeedFile was given."
        }
        $seedPath = Join-Path $temp "seed.b64"
        [System.IO.File]::WriteAllText($seedPath, $env:UPDATE_SIGNING_SEED.Trim() + "`n")
    }

    $outPath = Join-Path $root $Out
    New-Item -ItemType Directory -Force (Split-Path -Parent $outPath) | Out-Null

    Push-Location (Join-Path $root "release")
    try {
        & go run . sign -artifact $payloadPath -url $artifactUrl -version $Version `
            -build $Build -channel $Channel -seed-file $seedPath `
            -notes-url $NotesUrl -out $outPath
        if ($LASTEXITCODE -ne 0) { throw "Signing failed." }

        & go run . verify -file $outPath -seed-file $seedPath
        if ($LASTEXITCODE -ne 0) { throw "Manifest verification failed." }
    } finally {
        Pop-Location
    }

    if (-not $NoDeploy) {
        if (-not $env:CLOUDFLARE_API_TOKEN) {
            throw "CLOUDFLARE_API_TOKEN is unset."
        }

        # Deploying from a checkout must not roll back news edited through the
        # local editor; carry the live feed into this build.
        $newsUrl = "https://esper-api.esper-api-worker.workers.dev/v1/news.json"
        try {
            $news = (Invoke-WebRequest -Uri $newsUrl -UseBasicParsing -TimeoutSec 20).Content
            $newsPath = Join-Path $root "public/v1/news.json"
            New-Item -ItemType Directory -Force (Split-Path -Parent $newsPath) | Out-Null
            [System.IO.File]::WriteAllText(
                $newsPath,
                $news,
                (New-Object System.Text.UTF8Encoding($false))
            )
        } catch {
            Write-Warning "Could not preserve live news; deploying the repository copy: $($_.Exception.Message)"
        }

        & npx --yes wrangler@4 deploy
        if ($LASTEXITCODE -ne 0) { throw "wrangler deploy failed." }
    }

    Write-Host ""
    Write-Host "Release $Channel $Version build $Build"
    Write-Host "Artifact: $artifactUrl"
    Write-Host "Manifest: $Out"
    Write-Host $(if ($NoDeploy) { "Signed only (-NoDeploy)." } else { "Deployed." })
} finally {
    Remove-Item $temp -Recurse -Force -ErrorAction SilentlyContinue
}
