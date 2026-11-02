# Isolated portable file replacement and production-launch check. No installer.
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot = Split-Path -Parent $PSScriptRoot
$stage = [IO.Path]::GetFullPath((Join-Path $projectRoot 'output/portable-update-04-stable'))
if (-not $stage.StartsWith($projectRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Stage escaped workspace' }
if (Test-Path -LiteralPath $stage) { throw 'Refuse to overwrite an existing update fixture' }
$dataDirectory = Join-Path $projectRoot 'output/native-library-04'
$databasePath = Join-Path $dataDirectory 'library.db'
if (-not (Test-Path -LiteralPath $databasePath)) { throw 'Missing isolated migration fixture' }
$lastTestId = [int](Get-Content (Join-Path $projectRoot 'output/playwright/library04-pid.txt'))
$oldTestProcess = Get-Process -Id $lastTestId -ErrorAction SilentlyContinue
if ($oldTestProcess) {
    if ($oldTestProcess.Path -ne (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe')) { throw 'Unexpected process identity' }
    if (-not $oldTestProcess.WaitForExit(10000)) { throw 'Close the isolated acceptance process before file replacement' }
}
New-Item -ItemType Directory -Path $stage | Out-Null
Add-Type -AssemblyName System.IO.Compression.FileSystem
$payload = @('LICENSE','OTONEIGO.exe','README.txt','THIRD_PARTY_NOTICES.txt')
function Copy-PortablePayload([string]$ArchivePath) {
    $archive = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        if ((@($archive.Entries.FullName | Sort-Object) -join '|') -ne ($payload -join '|')) { throw 'Unexpected archive payload' }
        foreach ($entry in $archive.Entries) {
            $inputStream = $entry.Open()
            $outputStream = [IO.File]::Open((Join-Path $stage $entry.FullName), [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
            try { $inputStream.CopyTo($outputStream) } finally { $outputStream.Dispose(); $inputStream.Dispose() }
        }
    } finally { $archive.Dispose() }
}
$databaseBefore = (Get-FileHash -LiteralPath $databasePath -Algorithm SHA256).Hash
Copy-PortablePayload (Join-Path $projectRoot 'bin/OTONEIGO-0.3.0-windows-x64.zip')
Copy-PortablePayload (Join-Path $projectRoot 'bin/OTONEIGO-0.4.0-windows-x64.zip')
if ((Get-FileHash -LiteralPath $databasePath -Algorithm SHA256).Hash -ne $databaseBefore) { throw 'Replacing portable files changed database bytes' }
if ((Get-FileHash (Join-Path $stage 'OTONEIGO.exe') -Algorithm SHA256).Hash -ne (Get-FileHash (Join-Path $projectRoot 'bin/OTONEIGO.exe') -Algorithm SHA256).Hash) { throw 'Packaged program differs from production' }

Add-Type @'
using System;using System.Runtime.InteropServices;using System.Text;
public static class Portable04Window {
 public delegate bool Callback(IntPtr h,IntPtr p);
 [DllImport("user32.dll")] public static extern bool EnumWindows(Callback c,IntPtr p);
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",EntryPoint="PostMessageW")] public static extern bool PostMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
}
'@
$savedEnvironment = @{}
foreach ($settingName in @('OTONEIGO_DATA_DIR','HTTP_PROXY','HTTPS_PROXY')) { $savedEnvironment[$settingName] = [Environment]::GetEnvironmentVariable($settingName,'Process') }
try {
    $env:OTONEIGO_DATA_DIR = $dataDirectory
    $env:HTTP_PROXY = 'http://127.0.0.1:1'; $env:HTTPS_PROXY = 'http://127.0.0.1:1'
    $portableProcess = Start-Process -FilePath (Join-Path $stage 'OTONEIGO.exe') -PassThru -WindowStyle Hidden -RedirectStandardOutput (Join-Path $dataDirectory 'portable-stdout.log') -RedirectStandardError (Join-Path $dataDirectory 'portable-stderr.log')
} finally {
    foreach ($settingName in $savedEnvironment.Keys) { [Environment]::SetEnvironmentVariable($settingName,$savedEnvironment[$settingName],'Process') }
}
$nativeProcessId = $portableProcess.Id
# Keep the native process handle open before it exits. A lazily opened
# Start-Process object can otherwise lose the exit code after a fast close.
$nativeProcessHandle = $portableProcess.Handle
$windows = [Collections.Generic.List[IntPtr]]::new()
for ($attempt=0; $attempt -lt 100; $attempt++) {
    $windows.Clear()
    [Portable04Window]::EnumWindows({param($handle,$unused)
        $owner=0;[Portable04Window]::GetWindowThreadProcessId($handle,[ref]$owner)|Out-Null
        if ($owner -eq $nativeProcessId) {
            $title=[Text.StringBuilder]::new(256);$class=[Text.StringBuilder]::new(256)
            [Portable04Window]::GetWindowText($handle,$title,256)|Out-Null
            [Portable04Window]::GetClassName($handle,$class,256)|Out-Null
            if ($title.ToString() -eq 'OTONEIGO' -and $class.ToString() -eq 'WailsWebviewWindow') { $windows.Add($handle) }
        }
        return $true
    },[IntPtr]::Zero)|Out-Null
    if ($windows.Count -eq 1) { break }
    if ($portableProcess.HasExited) { throw 'Production program exited before creating its window' }
    Start-Sleep -Milliseconds 100
}
if ($windows.Count -ne 1) { throw 'Expected one production application window' }
# Start-Process Hidden keeps the real window hidden until explicitly shown.
# Target only this fixture's exact WebView HWND, without activating other apps.
# The native window can appear before its WebView/controller initialization.
# This is a launch/close smoke, not a substitute for the CDP UI acceptance.
Start-Sleep -Milliseconds 2000
$actualWindow = $windows[0]
for ($attempt=0; $attempt -lt 20; $attempt++) {
    [Portable04Window]::ShowWindow($actualWindow,4)|Out-Null
    if ([Portable04Window]::IsWindowVisible($actualWindow)) { break }
    Start-Sleep -Milliseconds 100
}
Start-Sleep -Milliseconds 1000
if (-not [Portable04Window]::IsWindowVisible($actualWindow)) { throw 'Production WebView window was not shown' }
if (-not [Portable04Window]::PostMessage($actualWindow,0x10,[IntPtr]::Zero,[IntPtr]::Zero)) { throw 'Production close message failed' }
if (-not $portableProcess.WaitForExit(10000)) { throw 'Production program did not close normally' }
$portableProcess.Refresh()
$nativeExitCode = $portableProcess.ExitCode
if ($null -eq $nativeExitCode) { throw 'Production exit code was not observable' }
if ($nativeExitCode -ne 0) { throw "Production program exit failed: $nativeExitCode" }
Invoke-ProjectToolchain {
    $script:durableStateText = (& go run ./.tools/musicfox-diagnosis/verify-library-04.go) -join "`n"
    Assert-ExitCode 'Inspect library after portable replacement'
}
$durableState = $script:durableStateText | ConvertFrom-Json
if (-not $durableState.pass -or $durableState.local_favorites -ne 6010 -or $durableState.local_history -lt 6010 -or $durableState.search_history -lt 1) { throw 'Portable update did not preserve fixture records' }
$report = [ordered]@{version='0.4.0';portableOnly=$true;previousPayloadReplaced=$true;oldVersionLaunched=$false;databaseUnchangedDuringFileReplacement=$true;productionWindowCreated=$true;windowShownForInitialization=$true;productionClosedNormally=$true;productionExitCode=$nativeExitCode;processOnlyOfflineProxy=$true;state=$durableState;pass=$true}
$report | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $projectRoot 'output/packaging/portable-update-0.4-report.json') -Encoding utf8
$report | ConvertTo-Json -Depth 5
