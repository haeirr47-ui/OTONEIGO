param([ValidateSet('recovery','indexed','unknown','lazy')][string]$Mode='recovery')
$ErrorActionPreference='Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot=Split-Path -Parent $PSScriptRoot
$profileName=if($Mode -eq 'indexed'){'recovery'}else{$Mode}
$profile=Join-Path $projectRoot "output/native-audit-052-$profileName"
$saved=@{}
foreach($name in @('OTONEIGO_DATA_DIR','HTTP_PROXY','HTTPS_PROXY')){$saved[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
try{
 $env:OTONEIGO_DATA_DIR=$profile;$env:HTTP_PROXY='http://127.0.0.1:1';$env:HTTPS_PROXY='http://127.0.0.1:1'
 $native=Start-Process -FilePath (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe') -PassThru -WindowStyle Hidden -RedirectStandardOutput "$profile/$Mode-stdout.log" -RedirectStandardError "$profile/$Mode-stderr.log"
 $null=$native.Handle
}finally{foreach($name in $saved.Keys){[Environment]::SetEnvironmentVariable($name,$saved[$name],'Process')}}
try{
 & node (Join-Path $PSScriptRoot 'native-audit052-acceptance.mjs') $Mode
 Assert-ExitCode 'Native recovery and indexed media regression'
}finally{
 $close=@'
import {nativeHarness} from "./scripts/native-harness.mjs";
const h=await nativeHarness();try{await h.page.evaluate(()=>window.__nativeTestRuntime.Window.Close());}catch(e){if(!/closed|Target|destroyed/.test(e.message))throw e;}await h.browser.close();
'@
 & node --input-type=module -e $close
 if(-not $native.WaitForExit(10000)){throw 'Native 0.5.2 host failed to close'}
}
$native.Refresh()
if($native.ExitCode -ne 0){throw "Native 0.5.2 exit failed: $($native.ExitCode)"}
& python (Join-Path $projectRoot '.tools/musicfox-diagnosis/inspect-audit052.py') $Mode
Assert-ExitCode 'Inspect quarantined data and unknown fields'
$report=@{mode=$Mode;native=$true;exitCode=$native.ExitCode;processOnlyOfflineProxy=$true;noInstaller=$true;pass=$true}
$report|ConvertTo-Json|Set-Content (Join-Path $projectRoot "output/playwright/native-audit052-$Mode-host-report.json") -Encoding utf8
$report|ConvertTo-Json
