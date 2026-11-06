# Deploys the Worker to Cloudflare. Requires Node.js and a completed
# `npx wrangler login` (or CLOUDFLARE_API_TOKEN in the environment).
$ErrorActionPreference = "Stop"

# Prefer the portable Node used to set this project up; fall back to PATH.
$portableNode = Join-Path $PSScriptRoot "..\tools\node-v26.13.0-win-x64"
if (Test-Path (Join-Path $portableNode "npx.cmd")) {
    $env:PATH = "$portableNode;" + $env:PATH
}

if (-not (Get-Command npx -ErrorAction SilentlyContinue)) {
    throw "Node.js/npx not found. Install Node.js LTS, then reopen PowerShell."
}

if (-not (Test-Path "node_modules")) {
    npm install
    if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
}

npx wrangler deploy
