$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
Invoke-ProjectToolchain {
    # gofmt has no Windows wildcard support; pass directories plus the root files explicitly.
    $rootGo = Get-ChildItem -File -Filter *.go | ForEach-Object { $_.Name }
    $unformatted = @(& gofmt -l cmd internal $rootGo)
    Assert-ExitCode 'Go formatting check'
    if ($unformatted.Count -gt 0) {
        $unformatted | ForEach-Object { Write-Host "Unformatted Go file: $_" }
        throw 'Go formatting check failed; run gofmt -w on the listed files.'
    }
    go test -timeout 60s ./...
    Assert-ExitCode 'Go tests'
    go vet ./...
    Assert-ExitCode 'Go vet'
    pnpm --dir frontend run format:check
    Assert-ExitCode 'Frontend format check'
    pnpm --dir frontend run lint
    Assert-ExitCode 'Frontend lint'
    pnpm --dir frontend test
    Assert-ExitCode 'Frontend tests'
}
& "$PSScriptRoot/build.ps1"
& "$PSScriptRoot/audit-dependencies.ps1"
