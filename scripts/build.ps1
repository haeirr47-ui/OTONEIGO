$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
Invoke-ProjectToolchain {
    # Go embed needs a directory before bindings can be generated on a clean checkout.
    if (-not (Test-Path 'frontend/dist/index.html')) {
        New-Item -ItemType Directory -Force 'frontend/dist' | Out-Null
        Set-Content -LiteralPath 'frontend/dist/index.html' -Value '<!doctype html><title>Preparing build</title>'
    }
    & '.tools/bin/wails3.exe' generate bindings -ts -i
    Assert-ExitCode 'Generate Wails TypeScript bindings'
    npm --prefix frontend run typecheck
    Assert-ExitCode 'Frontend typecheck'
    npm --prefix frontend run build
    Assert-ExitCode 'Frontend build'
    New-Item -ItemType Directory -Force 'bin' | Out-Null
    go build -trimpath -tags production -ldflags '-H=windowsgui -s -w' -o bin/OTONEIGO.exe .
    Assert-ExitCode 'Windows executable build'
    Write-Host "Built $(Join-Path $PWD 'bin/OTONEIGO.exe')"
}
