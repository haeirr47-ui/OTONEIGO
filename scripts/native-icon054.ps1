# 0.5.4 Windows resource acceptance: embedded icon, version table, manifest and
# the resulting DPI awareness of a real production WebView window. Isolated data
# directory, process-level unreachable proxy, no installer, no network calls.
# Every number written into the report is also a gate; host-dependent observations
# (DPI value, window size) are recorded and labelled as such, not asserted.
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot = Split-Path -Parent $PSScriptRoot
$version = Get-ProjectVersion
$exe = (Resolve-Path -LiteralPath "$projectRoot/bin/OTONEIGO.exe").Path
$dataDirectory = Join-Path $projectRoot 'output/native-icon-054'
if (-not $dataDirectory.StartsWith($projectRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Fixture escaped workspace' }
# This directory is created by this script alone and holds no user library, so a
# previous run's WebView profile and logs may be replaced instead of refused.
if (Test-Path -LiteralPath $dataDirectory) { Remove-Item -LiteralPath $dataDirectory -Recurse -Force }
New-Item -ItemType Directory -Path $dataDirectory | Out-Null
if (Get-Process -Name 'OTONEIGO' -ErrorAction SilentlyContinue) { throw 'Close running OTONEIGO instances before the smoke test' }

Add-Type @'
using System;using System.Runtime.InteropServices;using System.Text;
public static class Icon054Native {
 [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc c,IntPtr p);
 public delegate bool EnumWindowsProc(IntPtr h,IntPtr p);
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h,int n);
 [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
 [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out Rect r);
 [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
 [DllImport("user32.dll")] public static extern IntPtr GetWindowDpiAwarenessContext(IntPtr h);
 [DllImport("user32.dll")] public static extern bool AreDpiAwarenessContextsEqual(IntPtr a,IntPtr b);
 [StructLayout(LayoutKind.Sequential)] public struct Rect{public int Left,Top,Right,Bottom;}
 public static IntPtr PerMonitorV2 => new IntPtr(-4);
}
'@

# Presence is read from the PE resource directory. ExtractAssociatedIcon is kept as
# an additional shell-level check only: for an image without resources it returns
# the generic system icon, so it cannot prove anything on its own.
$inventory = Assert-WindowsResources -ExePath $exe -ExpectedIconEntries (Get-IconSizePlan).Count
$versionInfo = Assert-ExecutableVersion -ExePath $exe -Version $version
Add-Type -AssemblyName System.Drawing
try {
    $shellIcon = [System.Drawing.Icon]::ExtractAssociatedIcon($exe)
    $shellIconPixels = $shellIcon.Width
    $shellIcon.Dispose()
} catch {
    throw 'Explorer cannot extract an icon from the production image.'
}

function Find-MainWindow([int]$processId) {
    $found = [Collections.Generic.List[IntPtr]]::new()
    [Icon054Native]::EnumWindows({param($handle,$unused)
        $owner = 0; [Icon054Native]::GetWindowThreadProcessId($handle, [ref]$owner) | Out-Null
        if ($owner -eq $processId) {
            $title = [Text.StringBuilder]::new(256); $class = [Text.StringBuilder]::new(256)
            [Icon054Native]::GetWindowText($handle, $title, 256) | Out-Null
            [Icon054Native]::GetClassName($handle, $class, 256) | Out-Null
            if ($title.ToString() -eq 'OTONEIGO' -and $class.ToString() -eq 'WailsWebviewWindow') { $found.Add($handle) }
        }
        return $true
    }, [IntPtr]::Zero) | Out-Null
    return $found
}

$savedEnvironment = @{}
foreach ($settingName in @('OTONEIGO_DATA_DIR', 'HTTP_PROXY', 'HTTPS_PROXY')) {
    $savedEnvironment[$settingName] = [Environment]::GetEnvironmentVariable($settingName, 'Process')
}
try {
    $env:OTONEIGO_DATA_DIR = $dataDirectory
    $env:HTTP_PROXY = 'http://127.0.0.1:1'; $env:HTTPS_PROXY = 'http://127.0.0.1:1'
    $app = Start-Process -FilePath $exe -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $dataDirectory 'stdout.log') `
        -RedirectStandardError (Join-Path $dataDirectory 'stderr.log')
} finally {
    foreach ($settingName in $savedEnvironment.Keys) { [Environment]::SetEnvironmentVariable($settingName, $savedEnvironment[$settingName], 'Process') } }
# Keep the process handle open before it exits, otherwise a fast close can hide the
# exit code from a lazily opened Start-Process object.
$appHandle = $app.Handle
$windows = [Collections.Generic.List[IntPtr]]::new()
for ($attempt = 0; $attempt -lt 100; $attempt++) {
    $windows = Find-MainWindow $app.Id
    if ($windows.Count -eq 1) { break }
    if ($app.HasExited) { throw 'Production program exited before creating its window' }
    Start-Sleep -Milliseconds 100
}
if ($windows.Count -ne 1) { throw "Expected one production application window, found $($windows.Count)" }
$window = $windows[0]
Start-Sleep -Milliseconds 2000
$windowVisible = $false
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    [Icon054Native]::ShowWindow($window, 4) | Out-Null
    $windowVisible = [Icon054Native]::IsWindowVisible($window)
    if ($windowVisible) { break }
    Start-Sleep -Milliseconds 100
}
if (-not $windowVisible) { throw 'Production WebView window was not shown' }
# The manifest has to produce the same awareness Wails previously requested with the
# runtime API; if it did not, overlay placement would change with the release. This
# is asserted, unlike the DPI value and window size, which depend on the host display.
$perMonitorV2 = [Icon054Native]::AreDpiAwarenessContextsEqual([Icon054Native]::GetWindowDpiAwarenessContext($window), [Icon054Native]::PerMonitorV2)
if (-not $perMonitorV2) {
    throw 'The production window is not Per-Monitor V2 aware, so the manifest did not take effect.'
}
$dpi = [Icon054Native]::GetDpiForWindow($window)
$rect = New-Object Icon054Native+Rect
[Icon054Native]::GetWindowRect($window, [ref]$rect) | Out-Null
if (-not [Icon054Native]::PostMessage($window, 0x10, [IntPtr]::Zero, [IntPtr]::Zero)) { throw 'Production close message failed' }
if (-not $app.WaitForExit(10000)) { throw 'Production program did not close normally' }
$app.Refresh()
if ($app.ExitCode -ne 0) { throw "Production program exit failed: $($app.ExitCode)" }
$checks = [ordered]@{
    resourceKindsPresent = ($inventory['icon'] -eq (Get-IconSizePlan).Count -and $inventory['groupIcon'] -ge 1 -and $inventory['version'] -ge 1 -and $inventory['manifest'] -ge 1)
    versionResourceExact = ($versionInfo.ProductName -eq 'OTONEIGO' -and $versionInfo.ProductVersion -eq $version -and $versionInfo.FileVersion -eq $version)
    # The shell icon size is host/DPI dependent; only successful extraction is a gate.
    shellExtractedIcon = ($shellIconPixels -ge 16)
    perMonitorV2Context = [bool]$perMonitorV2
    windowShownAndInitialised = [bool]$windowVisible
    normalExitCodeZero = ($app.ExitCode -eq 0)
}
$report = [ordered]@{
    version = $version
    executableSha256 = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    resourceEntries = $inventory
    iconSizePlan = (Get-IconSizePlan)
    fileVersion = $versionInfo.FileVersion; productVersion = $versionInfo.ProductVersion
    productName = $versionInfo.ProductName; fileDescription = $versionInfo.FileDescription
    shellIconPixels = $shellIconPixels
    # Recorded host observations, not acceptance criteria.
    recordedWindowDpi = [int]$dpi
    recordedWindowSize = "$($rect.Right - $rect.Left)x$($rect.Bottom - $rect.Top)"
    isolatedDataDirectory = $true; processOnlyOfflineProxy = $true; exitCode = $app.ExitCode
    cdpUiAcceptance = 'covered by scripts/native-wails054.ps1, not by this launch smoke'
    checks = $checks
    pass = [bool](-not ($checks.Values -contains $false))
}
New-Item -ItemType Directory -Force (Join-Path $projectRoot 'output/packaging') | Out-Null
$report | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $projectRoot 'output/packaging/native-icon-054-report.json') -Encoding utf8
$report | ConvertTo-Json -Depth 5
if (-not $report.pass) { exit 1 }
