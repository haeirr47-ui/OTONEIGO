$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
Invoke-ProjectToolchain {
    New-Item -ItemType Directory -Force 'output/p0' | Out-Null
    $compiled = @(go list -deps ./...)
    Assert-ExitCode 'List compiled packages'
    $graph = @(go mod graph)
    Assert-ExitCode 'List module graph'
    $forbidden = '(?i)UnblockNeteaseMusic|firebase|go-musicfox/(go-musicfox|netease-music)'
    if (($compiled + $graph) -match $forbidden) { throw 'Forbidden dependency in compiled packages or module graph' }
    $compiled | Set-Content -Encoding utf8 'output/p0/go-deps.txt'
    $graph | Set-Content -Encoding utf8 'output/p0/go-mod-graph.txt'
    if (Test-Path 'bin/OTONEIGO.exe') {
        $buildInfo = @(go version -m bin/OTONEIGO.exe)
        Assert-ExitCode 'Inspect executable build information'
        if ($buildInfo -match $forbidden) { throw 'Forbidden module in executable' }
        if (-not ($buildInfo -match '-tags=production')) { throw 'Executable was not built with production tag' }
        $buildInfo | Set-Content -Encoding utf8 'output/p0/go-build-info.txt'
    }
    $moduleRows = @(go list -deps -f '{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}' .) |
        Where-Object { $_ -match '\|' } | Sort-Object -Unique
    Assert-ExitCode 'List executable modules'
    $inventory = @()
    $notices = [System.Text.StringBuilder]::new()
    foreach ($file in @('LICENSE', 'frontend/THIRD_PARTY_NOTICES.md', 'docs/netease-adapter.md')) {
        if (Test-Path -LiteralPath $file) {
            [void]$notices.AppendLine("===== $file =====")
            [void]$notices.AppendLine((Get-Content -Raw -LiteralPath $file))
        }
    }
    foreach ($row in $moduleRows) {
        $parts = $row.Split('|', 3)
        if ($parts[1] -eq '') { continue }
        $licenseFiles = @(Get-ChildItem -LiteralPath $parts[2] -File |
            Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE)(\..*)?$' })
        if ($licenseFiles.Count -eq 0) { throw "Missing license notice for $($parts[0])" }
        foreach ($file in $licenseFiles) {
            $inventory += [ordered]@{
                module = $parts[0]; version = $parts[1]; file = $file.Name
                sha256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            }
            [void]$notices.AppendLine("===== $($parts[0]) $($parts[1]) / $($file.Name) =====")
            [void]$notices.AppendLine((Get-Content -Raw -LiteralPath $file.FullName))
        }
    }
    # The production executable embeds the frontend bundle as well as Go code.
    $frontendInventory = @()
    $frontendPaths = @(& npm --prefix frontend ls --omit=dev --all --parseable)
    Assert-ExitCode 'List frontend runtime dependencies'
    if ($frontendPaths -match '(?i)firebase|UnblockNeteaseMusic') { throw 'Forbidden frontend runtime dependency' }
    foreach ($dependencyPath in ($frontendPaths | Sort-Object -Unique)) {
        if ($dependencyPath -notmatch '[\\/]node_modules[\\/]') { continue }
        $package = Get-Content -Raw -LiteralPath (Join-Path $dependencyPath 'package.json') | ConvertFrom-Json
        $frontendLicenses = @(Get-ChildItem -LiteralPath $dependencyPath -File |
            Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE)(\..*)?$' })
        if ($frontendLicenses.Count -eq 0) {
            # The npm runtime omits its notice; the same pinned Wails release's
            # full MIT notice is already included by the executable module audit.
            if ($package.name -eq '@wailsio/runtime' -and $package.version -eq '3.0.0' -and
                ($inventory | Where-Object { $_.module -eq 'github.com/wailsapp/wails/v3' -and $_.version -eq 'v3.0.0' })) {
                $frontendInventory += [ordered]@{ package = $package.name; version = $package.version; licenseSource = 'Wails Go module notice' }
                continue
            }
            throw "Missing frontend runtime license: $($package.name)"
        }
        foreach ($file in $frontendLicenses) {
            $frontendInventory += [ordered]@{ package = $package.name; version = $package.version; file = $file.Name; sha256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
            [void]$notices.AppendLine("===== frontend $($package.name) $($package.version) / $($file.Name) =====")
            [void]$notices.AppendLine((Get-Content -Raw -LiteralPath $file.FullName))
        }
    }
    foreach ($assetNotice in @('frontend/public/licenses/MaterialIcons-LICENSE.txt')) {
        if (Test-Path -LiteralPath $assetNotice) {
            [void]$notices.AppendLine("===== $assetNotice =====")
            [void]$notices.AppendLine((Get-Content -Raw -LiteralPath $assetNotice))
        }
    }
    $frontendInventory | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 'output/p0/frontend-license-inventory.json'
    $inventory | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 'output/p0/license-inventory.json'
    if (Test-Path -LiteralPath 'bin') {
        $notices.ToString() | Set-Content -Encoding utf8 'bin/THIRD_PARTY_NOTICES.txt'
        Copy-Item -LiteralPath 'LICENSE' -Destination 'bin/LICENSE' -Force
    }
    Write-Host "Dependency gate passed; $($inventory.Count) upstream license files recorded."
}
