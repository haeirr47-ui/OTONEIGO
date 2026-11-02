param([switch]$VisibleAuxiliaries)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
$desktopProfile=Join-Path $projectRoot 'output/native-desktop-05'
$oldProfile=[Environment]::GetEnvironmentVariable('OTONEIGO_DATA_DIR','Process')
try{
 [Environment]::SetEnvironmentVariable('OTONEIGO_DATA_DIR',$desktopProfile,'Process')
 $native=Start-Process -FilePath (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe') -PassThru -WindowStyle Hidden -RedirectStandardOutput "$desktopProfile/final-stdout.log" -RedirectStandardError "$desktopProfile/final-stderr.log"
 $native.Id|Set-Content "$desktopProfile/process-id.txt"
 $nativeHandle=$native.Handle
}finally{[Environment]::SetEnvironmentVariable('OTONEIGO_DATA_DIR',$oldProfile,'Process')}
& node (Join-Path $PSScriptRoot 'native-desktop05-lifecycle.mjs') restore
if($LASTEXITCODE -ne 0){throw 'Desktop placement restore failed'}
$closeMode=if($VisibleAuxiliaries){'close-visible'}else{'close-hidden'}
& node (Join-Path $PSScriptRoot 'native-desktop05-lifecycle.mjs') $closeMode
if($LASTEXITCODE -ne 0){throw 'Desktop hidden close failed'}
if(-not $native.WaitForExit(10000)){throw 'Hidden auxiliaries kept application alive'}
$native.Refresh()
if($null -eq $native.ExitCode -or $native.ExitCode -ne 0){throw "Native exit failed: $($native.ExitCode)"}
$report=[ordered]@{restartPreservedPlacement=$true;immediateCloseGeometryFlushed=$true;closeMode=$closeMode;exitCode=$native.ExitCode;pass=$true}
$reportName=if($VisibleAuxiliaries){'native-desktop05-restart-visible-report.json'}else{'native-desktop05-restart-report.json'}
$report|ConvertTo-Json|Set-Content (Join-Path $projectRoot "output/playwright/$reportName") -Encoding utf8
$report|ConvertTo-Json
