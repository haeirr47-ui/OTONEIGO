$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
Invoke-ProjectToolchain {
    go test -timeout 60s ./...
    Assert-ExitCode 'Go tests'
    go vet ./...
    Assert-ExitCode 'Go vet'
    npm --prefix frontend test
    Assert-ExitCode 'Frontend tests'
}
& "$PSScriptRoot/build.ps1"
& "$PSScriptRoot/audit-dependencies.ps1"
