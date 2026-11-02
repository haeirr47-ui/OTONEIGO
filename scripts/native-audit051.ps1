# Only an isolated test profile. No installer, screenshots or global settings.
$ErrorActionPreference='Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot=Split-Path -Parent $PSScriptRoot
$profile=Join-Path $projectRoot 'output/native-audit-051'
$executable=Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe'
$imageHash=(Get-FileHash $executable -Algorithm SHA256).Hash
function Test-ImageWritable {
 try{$f=[IO.File]::Open($executable,[IO.FileMode]::Open,[IO.FileAccess]::Write,([IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete));$f.Dispose();return $true}catch [IO.IOException]{return $false}
}
if(-not (Test-ImageWritable)){throw 'Acceptance executable was already in use'}
$saved=@{}
foreach($name in @('OTONEIGO_DATA_DIR','HTTP_PROXY','HTTPS_PROXY')){$saved[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
try{
 $env:OTONEIGO_DATA_DIR=$profile;$env:HTTP_PROXY='http://127.0.0.1:1';$env:HTTPS_PROXY='http://127.0.0.1:1'
 $native=Start-Process -FilePath $executable -PassThru -WindowStyle Hidden -RedirectStandardOutput "$profile/stdout.log" -RedirectStandardError "$profile/stderr.log"
 $null=$native.Handle
}finally{foreach($name in $saved.Keys){[Environment]::SetEnvironmentVariable($name,$saved[$name],'Process')}}
try{
 & node (Join-Path $PSScriptRoot 'native-audit051-acceptance.mjs')
 Assert-ExitCode 'Native audit regression'
 if(Test-ImageWritable){throw 'Windows allowed write access to running image'}
 $runningImageWriteBlocked=$true
}finally{
 $close=@'
import {nativeHarness} from "./scripts/native-harness.mjs";
const h=await nativeHarness();try{await h.page.evaluate(()=>window.__nativeTestRuntime.Window.Close());}catch(e){if(!/closed|Target|destroyed/.test(e.message))throw e;}await h.browser.close();
'@
 & node --input-type=module -e $close
 if(-not $native.WaitForExit(10000)){throw 'Native audit host did not exit'}
}
$native.Refresh()
if($native.ExitCode -ne 0){throw "Native audit exit failed: $($native.ExitCode)"}
if(-not (Test-ImageWritable)){throw 'Exited image remained in use'}
if((Get-FileHash $executable -Algorithm SHA256).Hash -ne $imageHash){throw 'Write-access probe modified image bytes'}
Invoke-ProjectToolchain {$script:databaseReport=(& go run ./.tools/musicfox-diagnosis/inspect-audit051.go)-join "`n";Assert-ExitCode 'Inspect actual native data and cached MP3 samples'}
$database=$script:databaseReport|ConvertFrom-Json
$report=[ordered]@{native=$true;exitCode=$native.ExitCode;processOnlyOfflineProxy=$true;writeAccessBefore=$true;runningImageWriteBlocked=$runningImageWriteBlocked;writeAccessAfter=$true;probeDidNotChangeImage=$true;installerBuiltOrRun=$false;database=$database;pass=$true}
$report|ConvertTo-Json -Depth 6|Set-Content (Join-Path $projectRoot 'output/playwright/native-audit051-host-report.json') -Encoding utf8
$report|ConvertTo-Json -Depth 6
