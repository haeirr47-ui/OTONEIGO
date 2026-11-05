$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot = Split-Path -Parent $PSScriptRoot
# frontend/package.json owns the release version. Every place the Windows
# resources repeat it is compared here, because Explorer, the ZIP name and the
# package gate each read a different one of these fields.
$frontendVersion = Get-ProjectVersion
$resourceInfo = Get-Content -Raw -LiteralPath "$projectRoot/packaging/windows/info.json" | ConvertFrom-Json
[xml]$resourceManifest = Get-Content -Raw -LiteralPath "$projectRoot/packaging/windows/wails.exe.manifest"
# The language key is the en-US table used by the version resource.
$declaredVersions = [ordered]@{
    'info.json fixed.file_version' = $resourceInfo.fixed.file_version
    'info.json fixed.product_version' = $resourceInfo.fixed.product_version
    'info.json info.0409.FileVersion' = $resourceInfo.info.'0409'.FileVersion
    'info.json info.0409.ProductVersion' = $resourceInfo.info.'0409'.ProductVersion
    'manifest assemblyIdentity' = $resourceManifest.assembly.assemblyIdentity.version
}
$mismatched = @($declaredVersions.Keys | Where-Object { $declaredVersions[$_] -ne $frontendVersion })
if ($mismatched.Count -gt 0) {
    throw "Release version mismatch against frontend/package.json ($frontendVersion): " +
        (($mismatched | ForEach-Object { "$_=$($declaredVersions[$_])" }) -join ', ')
}
# The packaged README is a payload file, so its stated version is gated too. The
# check itself lives in toolchain.ps1 because the packaging path ships that file
# without running this build.
Assert-PayloadVersionDocuments -Version $frontendVersion
$iconSizes = Get-IconSizePlan
Invoke-ProjectToolchain {
    # Go embed needs a directory before bindings can be generated on a clean checkout.
    if (-not (Test-Path 'frontend/dist/index.html')) {
        New-Item -ItemType Directory -Force 'frontend/dist' | Out-Null
        Set-Content -LiteralPath 'frontend/dist/index.html' -Value '<!doctype html><title>Preparing build</title>'
    }
    & '.tools/bin/wails3.exe' generate bindings -ts -i
    Assert-ExitCode 'Generate Wails TypeScript bindings'
    pnpm --dir frontend run typecheck
    Assert-ExitCode 'Frontend typecheck'
    pnpm --dir frontend run build
    Assert-ExitCode 'Frontend build'
    New-Item -ItemType Directory -Force 'bin' | Out-Null
    # Standard Wails resource pipeline: PNG master -> multi-size ICO -> .syso.
    # The .syso is transient: leaving it behind would let any later `go build` or
    # `go test` link resources that this run did not just verify.
    try {
        & '.tools/bin/wails3.exe' generate icons -input packaging/windows/appicon.png -windowsfilename packaging/windows/icon.ico -macfilename '' -sizes ($iconSizes -join ',')
        Assert-ExitCode 'Generate Windows icon'
        & '.tools/bin/wails3.exe' generate syso -arch amd64 -icon packaging/windows/icon.ico -manifest packaging/windows/wails.exe.manifest -info packaging/windows/info.json -out wails_windows_amd64.syso
        Assert-ExitCode 'Generate Windows resources'
        go build -trimpath -tags production -ldflags '-H=windowsgui -s -w' -o bin/OTONEIGO.exe .
        Assert-ExitCode 'Windows executable build'
    } finally {
        Remove-Item -LiteralPath 'wails_windows_amd64.syso' -Force -ErrorAction SilentlyContinue
    }
    # The resources must survive into the image. Presence is read from the PE
    # resource directory, because the shell returns its own generic icon for an
    # image that carries none.
    $exe = (Resolve-Path -LiteralPath 'bin/OTONEIGO.exe').Path
    $inventory = Assert-WindowsResources -ExePath $exe -ExpectedIconEntries $iconSizes.Count
    Assert-ExecutableVersion -ExePath $exe -Version $frontendVersion | Out-Null
    # Interpolating the dictionary directly would print its type name, not the counts.
    $inventoryText = ($inventory.Keys | Sort-Object | ForEach-Object { "$_=$($inventory[$_])" }) -join ', '
    Write-Host "Built $(Join-Path $PWD 'bin/OTONEIGO.exe') with resources [$inventoryText] and version $frontendVersion"
}
