[CmdletBinding()]
param([switch]$SkipBuild, [switch]$PortableOnly = $true)

$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot = Split-Path -Parent $PSScriptRoot
# frontend/package.json owns the release version; build.ps1 refuses to embed Windows
# resources that disagree with it, and the executable check below refuses an image
# that still claims the previous release, so the ZIP name and the EXE cannot drift.
$version = Get-ProjectVersion
# The README travels in the payload, so it must state the same release even when
# `-SkipBuild` reuses an executable this run did not produce.
Assert-PayloadVersionDocuments -Version $version
$nsisVersion = '3.13'
$nsisHash = 'ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8'
$nsisUrl = 'https://downloads.sourceforge.net/project/nsis/NSIS%203/3.13/nsis-3.13.zip'
Push-Location $projectRoot
try {
    if (-not $SkipBuild) {
        & "$PSScriptRoot/build.ps1"
        & "$PSScriptRoot/audit-dependencies.ps1"
    }
    foreach ($name in @('OTONEIGO.exe', 'LICENSE', 'THIRD_PARTY_NOTICES.txt')) {
        $file = Get-Item -LiteralPath "bin/$name" -ErrorAction Stop
        if ($file.Length -eq 0 -or $file.PSIsContainer) { throw "Missing or empty package input: $name" }
    }
    $metadata = @()
    Invoke-ProjectToolchain {
        $script:packageBuildInfo = @(go version -m bin/OTONEIGO.exe)
        Assert-ExitCode 'Read package executable metadata'
    }
    $metadata = $script:packageBuildInfo
    if (-not ($metadata -match 'build\s+GOOS=windows') -or
        -not ($metadata -match 'build\s+GOARCH=amd64') -or
        -not ($metadata -match 'build\s+-tags=production\s*$') -or
        ($metadata -match '(?i)p0debug|UnblockNeteaseMusic|firebase|go-musicfox/(go-musicfox|netease-music)')) {
        throw 'Package requires a Windows amd64 production executable without debug tags or forbidden modules.'
    }
    # -SkipBuild must not ship an older image, whose icon and version resource would
    # still claim the previous release. Both are checked: the version table alone
    # cannot prove that this build embedded the icon and manifest.
    $exePath = (Resolve-Path -LiteralPath 'bin/OTONEIGO.exe').Path
    $exeVersion = Assert-ExecutableVersion -ExePath $exePath -Version $version
    $resourceInventory = Assert-WindowsResources -ExePath $exePath -ExpectedIconEntries (Get-IconSizePlan).Count
    $stem = "OTONEIGO-$version-windows-x64"
    # Only these four explicit inputs enter either payload. Never package a directory recursively.
    $stage = Join-Path $projectRoot "output/packaging/$stem"
    New-Item -ItemType Directory -Force $stage | Out-Null
    $payload = @('OTONEIGO.exe', 'LICENSE', 'THIRD_PARTY_NOTICES.txt', 'README.txt')
    foreach ($name in $payload[0..2]) { Copy-Item -LiteralPath "bin/$name" -Destination "$stage/$name" -Force }
    if (-not $PortableOnly) {
        Add-Content -LiteralPath "$stage/THIRD_PARTY_NOTICES.txt" -Value (Get-Content -Raw -LiteralPath 'installer/nsis-notice.txt') -Encoding utf8
    }
    Copy-Item -LiteralPath 'installer/README.txt' -Destination "$stage/README.txt" -Force
    $zip = Join-Path $projectRoot "bin/$stem.zip"
    Compress-Archive -LiteralPath @($payload | ForEach-Object { "$stage/$_" }) -DestinationPath $zip -Force
    $artifacts = @($zip)
    if (-not $PortableOnly) {
        $archive = Join-Path $projectRoot ".tools/nsis-$nsisVersion.zip"
        if (-not (Test-Path -LiteralPath $archive)) {
            # Node uses normal TLS validation, including on hosts where curl's CRL lookup is offline.
            New-Item -ItemType Directory -Force '.tools' | Out-Null
            $download = @'
const fs = require('node:fs');
const crypto = require('node:crypto');
(async () => {
  const [url, target, expected] = process.argv.slice(1);
  const response = await fetch(url, {signal: AbortSignal.timeout(120000)});
  if (!response.ok) throw new Error(`NSIS download: HTTP ${response.status}`);
  const bytes = Buffer.from(await response.arrayBuffer());
  if (crypto.createHash('sha256').update(bytes).digest('hex') !== expected) throw new Error('NSIS SHA-256 mismatch');
  fs.writeFileSync(target, bytes);
})().catch(error => { console.error(error.message); process.exitCode = 1; });
'@
            & node -e $download $nsisUrl $archive $nsisHash
            Assert-ExitCode 'Download verified portable NSIS'
        }
        if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $nsisHash) {
            throw 'Cached NSIS archive SHA-256 mismatch. Remove the corrupt archive and retry.'
        }
        Expand-Archive -LiteralPath $archive -DestinationPath '.tools' -Force
        $compiler = Join-Path $projectRoot ".tools/nsis-$nsisVersion/makensis.exe"
        $compilerVersion = & $compiler /VERSION
        Assert-ExitCode 'Inspect NSIS version'
        if ($compilerVersion.Trim() -ne "v$nsisVersion") { throw "Unexpected NSIS version: $compilerVersion" }
        $setup = Join-Path $projectRoot "bin/$stem-setup.exe"
        & $compiler /NOCONFIG /INPUTCHARSET UTF8 /WX /V3 "/DPAYLOAD=$stage" "/DOUTPUT=$setup" 'installer/windows.nsi'
        Assert-ExitCode 'Compile Windows installer'
        $artifacts += $setup
    }
    $manifest = Join-Path $projectRoot "bin/$stem$(if ($PortableOnly) { '-portable' }).sha256"
    @($artifacts | ForEach-Object {
        "{0}  {1}" -f (Get-FileHash -LiteralPath $_ -Algorithm SHA256).Hash.ToLowerInvariant(), (Split-Path -Leaf $_)
    }) | Set-Content -LiteralPath $manifest -Encoding ascii
    $metadata | Set-Content -LiteralPath 'output/packaging/go-build-info.txt' -Encoding utf8
    # Reproducible final check, recorded as an artifact instead of only asserted:
    # the archive must hold exactly the whitelisted names, and every entry must be
    # byte-equal to the file it was staged from. THIRD_PARTY_NOTICES.txt legitimately
    # differs from bin/ when the NSIS notice is appended, so that pair is only
    # compared in the portable build.
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $opened = [IO.Compression.ZipFile]::OpenRead($zip)
    try {
        $entries = @($opened.Entries | ForEach-Object { $_.FullName })
        if ((($entries | Sort-Object) -join '|') -ne (($payload | Sort-Object) -join '|')) {
            throw "Unexpected archive payload: $($entries -join ', ')"
        }
        $entryHashes = @{}
        foreach ($entry in $opened.Entries) {
            $stream = $entry.Open()
            try { $digest = [Security.Cryptography.SHA256]::Create().ComputeHash($stream) }
            finally { $stream.Dispose() }
            $entryHashes[$entry.FullName] = [BitConverter]::ToString($digest).Replace('-', '').ToLowerInvariant()
        }
    } finally { $opened.Dispose() }
    $inputSources = @{
        'OTONEIGO.exe' = 'bin/OTONEIGO.exe'; 'LICENSE' = 'bin/LICENSE'
        'THIRD_PARTY_NOTICES.txt' = 'bin/THIRD_PARTY_NOTICES.txt'; 'README.txt' = 'installer/README.txt'
    }
    $inputHashes = @{}
    foreach ($name in $payload) {
        $inputHashes[$name] = (Get-FileHash -LiteralPath $inputSources[$name] -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($entryHashes[$name] -ne (Get-FileHash -LiteralPath (Join-Path $stage $name) -Algorithm SHA256).Hash.ToLowerInvariant()) {
            throw "Archive entry $name does not match the staged payload."
        }
        if ($PortableOnly -and $entryHashes[$name] -ne $inputHashes[$name]) {
            throw "Archive entry $name does not match its package input."
        }
    }
    # The report is the artifact reviewers compare against the tree, so it pins the
    # gate scripts it was produced from, not just the payload. A later edit to any
    # of them changes its hash and makes a stale report detectable instead of
    # plausible. These are the versions in the tree at report time; `-SkipBuild`
    # records build.ps1 too, which defines what a full build would have accepted.
    $gateHashes = [ordered]@{}
    foreach ($script in @('scripts/toolchain.ps1', 'scripts/build.ps1', 'scripts/audit-dependencies.ps1', 'scripts/package-windows.ps1')) {
        $gateHashes[$script] = (Get-FileHash -LiteralPath $script -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    $finalReport = [ordered]@{
        version = $version; portableOnly = [bool]$PortableOnly
        archive = (Split-Path -Leaf $zip); archiveBytes = (Get-Item -LiteralPath $zip).Length
        archiveSHA256 = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
        checksumManifest = (Split-Path -Leaf $manifest)
        payload = @($payload); entrySHA256 = $entryHashes; inputSHA256 = $inputHashes
        executableProductVersion = $exeVersion.ProductVersion; resourceEntries = $resourceInventory
        gateScriptSHA256 = $gateHashes
        installerBuilt = (-not $PortableOnly); codeSigned = $false; uploadedToRemote = $false
        notReRun = @('portable overwrite update', 'older version launch', 'installer lifecycle', 'multi-monitor and hibernation')
    }
    $finalReport | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $projectRoot "output/packaging/portable-$version-final-report.json") -Encoding utf8
    Write-Host "Packaged $($artifacts -join ', '); checksums: $manifest; final report: output/packaging/portable-$version-final-report.json"
} finally { Pop-Location }
