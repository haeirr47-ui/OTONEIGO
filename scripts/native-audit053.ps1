$ErrorActionPreference='Stop'
. "$PSScriptRoot/toolchain.ps1"
$projectRoot=Split-Path -Parent $PSScriptRoot
$auditDataDir=Join-Path $projectRoot 'output/native-audit-053'
$saved=@{}
foreach($name in @('OTONEIGO_DATA_DIR','HTTP_PROXY','HTTPS_PROXY')){$saved[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
try{
 $env:OTONEIGO_DATA_DIR=$auditDataDir;$env:HTTP_PROXY='http://127.0.0.1:1';$env:HTTPS_PROXY='http://127.0.0.1:1'
 $native=Start-Process -FilePath (Join-Path $projectRoot 'bin/OTONEIGO-acceptance.exe') -PassThru -WindowStyle Hidden -RedirectStandardOutput "$auditDataDir/stdout.log" -RedirectStandardError "$auditDataDir/stderr.log"
 $null=$native.Handle
}finally{foreach($name in $saved.Keys){[Environment]::SetEnvironmentVariable($name,$saved[$name],'Process')}}
try{
 & node (Join-Path $PSScriptRoot 'native-audit053-acceptance.mjs')
 Assert-ExitCode 'Native 0.5.3 polling and recovery notice'
}finally{
 $close=@'
import {nativeHarness} from "./scripts/native-harness.mjs";
const h=await nativeHarness();try{await h.page.evaluate(()=>window.__nativeTestRuntime.Window.Close());}catch(e){if(!/closed|Target|destroyed/.test(e.message))throw e;}await h.browser.close();
'@
 & node --input-type=module -e $close
 if(-not $native.WaitForExit(10000)){throw 'Native 0.5.3 host failed to close'}
}
$native.Refresh()
if($native.ExitCode -ne 0){throw "Native 0.5.3 exit failed: $($native.ExitCode)"}
$inspect=@'
from pathlib import Path
import sqlite3,json,hashlib
p=Path('output/native-audit-053');d=sqlite3.connect(p/'library.db');assert d.execute('PRAGMA user_version').fetchone()[0]==7
assert d.execute('PRAGMA integrity_check').fetchone()[0]=='ok';assert d.execute("SELECT count(*) FROM local_record_quarantine WHERE source_table='local_favorites'").fetchone()[0]==2
s=json.loads(d.execute('SELECT metadata FROM local_settings WHERE id=1').fetchone()[0]);assert s['preferredQuality']=='hires' and s['futureSetting']['retain'];d.close()
r=json.loads(Path('output/playwright/native-audit053-report.json').read_text(encoding='utf-8'));manifest=p/'cache/audio/manifest.json';assert hashlib.sha256(manifest.read_bytes()).hexdigest()==r['nativeRecency']['manifestSHA256'];assert abs(manifest.stat().st_mtime_ns/1e6-r['nativeRecency']['manifestMtimeMs'])<1
recency=json.loads((p/'cache/audio/recency.json').read_text(encoding='utf-8'));entries=json.loads(manifest.read_text(encoding='utf-8'));assert recency
for k,v in recency.items():assert entries[k]['Generation']==v['Generation'] and entries[k]['LastAccess']==v['BaseAccess']
print(json.dumps({'schema':7,'integrity':'ok','archivesRetained':2,'unknownFieldsPreserved':True,'recencySurvivesClose':True,'immutableManifestNotRewrittenAtClose':True,'pass':True}))
'@
$inspection=(& python -c $inspect)-join "`n"
Assert-ExitCode 'Inspect native 0.5.3 persisted state'
$report=@{native=$true;exitCode=$native.ExitCode;processOnlyOfflineProxy=$true;inspection=($inspection|ConvertFrom-Json);pass=$true}
$report|ConvertTo-Json -Depth 4|Set-Content (Join-Path $projectRoot 'output/playwright/native-audit053-host-report.json') -Encoding utf8
$report|ConvertTo-Json -Depth 4
