$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    if (-not (Get-Command go -ErrorAction SilentlyContinue) -and -not (Test-Path '.tools/go/bin/go.exe')) {
        New-Item -ItemType Directory -Force '.tools' | Out-Null
        $filename = 'go1.27.3.windows-amd64.zip'
        $checksum = 'f26081f438319757f8a1b46d7d0b2c5ec7583d4038e485e0aa725cfde7885fff'
        curl.exe -fSL "https://go.dev/dl/$filename" -o '.tools/go.zip'
        Assert-ExitCode 'Download official Go compiler'
        if ((Get-FileHash '.tools/go.zip' -Algorithm SHA256).Hash.ToLowerInvariant() -ne $checksum) {
            throw 'Official Go archive checksum mismatch'
        }
        Expand-Archive -LiteralPath '.tools/go.zip' -DestinationPath '.tools' -Force
    }
} finally { Pop-Location }
Invoke-ProjectToolchain {
    go version
    go mod download
    Assert-ExitCode 'Download Go modules'
    go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
    Assert-ExitCode 'Build project-local Wails CLI'
    if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) {
        throw 'pnpm 12 is required; install it before running bootstrap.'
    }
    pnpm --dir frontend install --frozen-lockfile
    Assert-ExitCode 'Install pinned frontend dependencies (pnpm)'
}
