$ErrorActionPreference = 'Stop'

function Invoke-ProjectToolchain {
    param([Parameter(Mandatory)][scriptblock]$Action)
    $projectRoot = Split-Path -Parent $PSScriptRoot
    $names = @('PATH', 'GOPATH', 'GOMODCACHE', 'GOCACHE', 'GOBIN', 'CGO_ENABLED')
    $previous = @{}
    foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
    Push-Location $projectRoot
    try {
        if (Test-Path -LiteralPath '.tools/go/bin/go.exe') {
            $env:PATH = "$(Join-Path $projectRoot '.tools/go/bin');$env:PATH"
        }
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
            throw 'Go is missing. Run scripts/bootstrap.ps1 first.'
        }
        $env:GOPATH = Join-Path $projectRoot '.tools/gopath'
        $env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
        $env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
        $env:GOBIN = Join-Path $projectRoot '.tools/bin'
        $env:CGO_ENABLED = '0'
        & $Action
    } finally {
        Pop-Location
        foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
    }
}

function Assert-ExitCode([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed (exit $LASTEXITCODE)" }
}
