# 0.5.4 / Wails 3.1.0 batch acceptance driver. Runs the whole gate chain and the
# native matrices in the order a release review needs, then aggregates the reports
# each script writes. It does not re-implement any assertion: the historical scripts
# stay the source of truth, and this file only sequences them, resets the fixtures
# they leave dirty, and reports honestly.
#
# -ColdCache restores the desktop05 fixture without its audio cache, so live
# Netease/GD requests are exercised. On hosts where anonymous Netease playback is
# refused, that path fails the matrix's source assertion for service reasons; the
# default warm run is what this batch recorded.
param([switch]$ColdCache)
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot = Split-Path -Parent $PSScriptRoot
$version = Get-ProjectVersion
$desktopProfile = Join-Path $projectRoot 'output/native-desktop-05'
$auditProfile = Join-Path $projectRoot 'output/native-audit-053'
$archiveDir = Join-Path $projectRoot 'output/native-wails054-archive'
if (Get-Process -Name 'OTONEIGO-acceptance' -ErrorAction SilentlyContinue) { throw 'Close the acceptance host first' }
if (Get-Process -Name 'OTONEIGO' -ErrorAction SilentlyContinue) { throw 'Close running OTONEIGO instances first' }

# 1. Full gate: formatting, Go tests/vet, frontend format/lint/tests, bindings,
#    production build with resources, dependency and licence audit.
# $LASTEXITCODE only tracks native commands; clear it so a .ps1 gate cannot
# inherit a stale value from an earlier tolerated failure.
$global:LASTEXITCODE = 0
& "$PSScriptRoot/verify.ps1"
Assert-ExitCode 'scripts/verify.ps1'

# 2. Portable ZIP from that build, plus the four-file/input hash report.
$global:LASTEXITCODE = 0
& "$PSScriptRoot/package-windows.ps1" -SkipBuild
Assert-ExitCode 'scripts/package-windows.ps1'
$packageReport = Get-Content -Raw -LiteralPath (Join-Path $projectRoot "output/packaging/portable-$version-final-report.json") | ConvertFrom-Json

# 3. Production launch smoke on the packaged image.
$global:LASTEXITCODE = 0
& "$PSScriptRoot/native-icon054.ps1"
Assert-ExitCode 'scripts/native-icon054.ps1'
$iconReport = Get-Content -Raw -LiteralPath (Join-Path $projectRoot 'output/packaging/native-icon-054-report.json') | ConvertFrom-Json

# 4. Acceptance binary: the production build plus the p0debug CDP listener.
Invoke-ProjectToolchain {
    go build -trimpath -tags 'production,p0debug' -ldflags '-H=windowsgui -s -w' -o bin/OTONEIGO-acceptance.exe .
    Assert-ExitCode 'Windows acceptance executable build'
}

# 5. Restore the fixtures the historical scripts leave dirty.
& python (Join-Path $PSScriptRoot 'native-wails054-fixture.py') `
    --audit053-profile $auditProfile --desktop05-profile $desktopProfile --archive-dir $archiveDir `
    $(if ($ColdCache) { '--cold-cache' })
Assert-ExitCode 'Reset acceptance fixtures'

# 6. Offline polling / recovery / recency matrix.
$global:LASTEXITCODE = 0
& "$PSScriptRoot/native-audit053.ps1"
Assert-ExitCode 'scripts/native-audit053.ps1'
$auditReport = Get-Content -Raw -LiteralPath (Join-Path $projectRoot 'output/playwright/native-audit053-host-report.json') | ConvertFrom-Json

$closeHost = @'
import {nativeHarness} from "./scripts/native-harness.mjs";
const h=await nativeHarness();try{await h.page.evaluate(()=>window.__nativeTestRuntime.Window.Close());}catch(e){if(!/closed|Target|destroyed/.test(e.message))throw e;}await h.browser.close();
'@
function Invoke-AcceptanceHost([string]$logPrefix) {
    $saved = [Environment]::GetEnvironmentVariable('OTONEIGO_DATA_DIR', 'Process')
    try {
        [Environment]::SetEnvironmentVariable('OTONEIGO_DATA_DIR', $desktopProfile, 'Process')
        $native = Start-Process -FilePath (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe') -PassThru -WindowStyle Hidden `
            -RedirectStandardOutput (Join-Path $desktopProfile "$logPrefix-stdout.log") `
            -RedirectStandardError (Join-Path $desktopProfile "$logPrefix-stderr.log")
        $null = $native.Handle
        $native.Id | Set-Content (Join-Path $desktopProfile 'process-id.txt')
    } finally {
        [Environment]::SetEnvironmentVariable('OTONEIGO_DATA_DIR', $saved, 'Process')
    }
    return $native
}

# 7. Desktop overlays, tray, taskbar and notification matrix, then the native window
#    styles. The acceptance script is allowed to fail here without aborting the run,
#    because its live-request stage assertions are unsatisfiable on some hosts and
#    the remaining phases still produce the native evidence this batch needs.
$host1 = Invoke-AcceptanceHost 'wails054'
$acceptanceExit = 0
$windowStyleExit = 0
try {
    try {
        & node (Join-Path $PSScriptRoot 'native-desktop05-acceptance.mjs')
        $acceptanceExit = $LASTEXITCODE
    } catch {
        $acceptanceExit = if ($LASTEXITCODE) { $LASTEXITCODE } else { 1 }
    }
    # The tolerated failure above can leave $LASTEXITCODE=1. Clear it so the
    # remaining scripted gates are not judged by that stale value.
    $global:LASTEXITCODE = 0
    & "$PSScriptRoot/native-desktop05-windowstyle.ps1"
    $windowStyleExit = $LASTEXITCODE
    if ($windowStyleExit -ne 0) { throw 'native-desktop05-windowstyle.ps1 failed' }
} finally {
    & node --input-type=module -e $closeHost
    $closeExit = $LASTEXITCODE
    if (-not $host1.WaitForExit(20000)) { throw 'Acceptance host did not close after Window.Close' }
}
$host1.Refresh()
if ($closeExit -ne 0) { throw 'Closing the acceptance host reported a failure' }
if ($host1.ExitCode -ne 0) { throw "Acceptance host exit failed: $($host1.ExitCode)" }

# 8. Placement restore across a restart; this script manages its own host.
& "$PSScriptRoot/native-desktop05-restart.ps1"
Assert-ExitCode 'scripts/native-desktop05-restart.ps1'

$desktop05 = Get-Content -Raw -LiteralPath (Join-Path $projectRoot 'output/playwright/native-desktop05-report.json') | ConvertFrom-Json
$windowStyle = Get-Content -Raw -LiteralPath (Join-Path $projectRoot 'output/playwright/native-desktop05-windowstyle-report.json') | ConvertFrom-Json
$restart = Get-Content -Raw -LiteralPath (Join-Path $projectRoot 'output/playwright/native-desktop05-restart-report.json') | ConvertFrom-Json
$wailsVersion = @(Invoke-ProjectToolchain { go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3 })
$report = [ordered]@{
    version = $version
    wailsGo = $wailsVersion[0]
    coldCache = [bool]$ColdCache
    packageArchive = $packageReport.archive; packageArchiveSHA256 = $packageReport.archiveSHA256
    productionExecutableSHA256 = $packageReport.inputSHA256.'OTONEIGO.exe'
    acceptanceExecutableSHA256 = (Get-FileHash -LiteralPath (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
    icon = $iconReport
    audit053 = $auditReport
    desktop05ExitCode = $acceptanceExit
    windowStyleExitCode = $windowStyleExit
    desktop05 = $desktop05
    windowStyle = $windowStyle
    restart = $restart
}
# Every stage contributes to pass. A known-unsatisfiable assertion still makes the
# batch report false, with the reason spelled out next to it; it is never folded
# into "passed" by leaving it out of the conjunction.
$report.pass = [bool](
    $iconReport.pass -and $auditReport.pass -and $desktop05.pass -and
    $windowStyle.pass -and $restart.pass -and $acceptanceExit -eq 0 -and $windowStyleExit -eq 0
)
$report.knownFailures = @()
if ($acceptanceExit -ne 0) {
    $report.knownFailures += "native-desktop05-acceptance.mjs exit $acceptanceExit at $($desktop05.location) during phase '$($desktop05.phase)': its request-stage counts need live Netease/GD fetches, which a warm audio cache serves from disk and anonymous requests currently answer UNAUTHORIZED."
}
$report | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $projectRoot 'output/playwright/native-wails054-report.json') -Encoding utf8
$report | ConvertTo-Json -Depth 4
if (-not $report.pass) { exit 1 }
