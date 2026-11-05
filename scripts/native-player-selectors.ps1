param()
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
$source=Join-Path $root 'output/native-desktop-05'
$stamp=Get-Date -Format 'yyyyMMdd-HHmmss'
$profile=Join-Path $root "output/native-player-selectors-$stamp"
$output=Join-Path $root 'output/playwright'
$exe=Join-Path $root 'bin/OTONEIGO-acceptance.exe'
if(-not (Test-Path (Join-Path $source 'library.db'))){throw "Native selector fixture missing: $source/library.db"}
if(-not (Test-Path $exe)){throw "Acceptance binary missing: $exe (root must build p0debug acceptance first)"}
foreach($path in @((Join-Path $source 'cache/audio/manifest.json'),(Join-Path $source 'cache/resources/manifest.json'))){if(-not(Test-Path $path)){throw "Native selector cache manifest missing: $path"}}
$listener=$null
try{$listener=[System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback,9225);$listener.Start()}catch{throw 'Port 9225 is already occupied; refusing to attach to any existing WebView2/app process'}finally{if($listener){$listener.Stop()}}
New-Item -ItemType Directory -Path $profile,$output,(Join-Path $profile 'cache') -Force|Out-Null
Copy-Item (Join-Path $source 'cache/audio') (Join-Path $profile 'cache/audio') -Recurse
Copy-Item (Join-Path $source 'cache/resources') (Join-Path $profile 'cache/resources') -Recurse
$dbSource=Join-Path $source 'library.db';$dbTarget=Join-Path $profile 'library.db'
$sourceHashes=@{}
$sourceHashes['library.db']=(Get-FileHash $dbSource -Algorithm SHA256).Hash
foreach($file in (Get-ChildItem (Join-Path $source 'cache') -File -Recurse)){$relative=$file.FullName.Substring((Join-Path $source 'cache').Length+1);$sourceHashes["cache/$relative"]=(Get-FileHash $file.FullName -Algorithm SHA256).Hash}
$python=@'
import json, sqlite3, sys
src, dst = sys.argv[1:]
source = sqlite3.connect(src)
target = sqlite3.connect(dst)
source.backup(target)
row = target.execute('SELECT revision, metadata FROM queue_snapshot WHERE id=1').fetchone()
if row is None: raise RuntimeError('fixture queue_snapshot missing')
revision, raw = row
q = json.loads(raw)
if not q.get('entries'): raise RuntimeError('fixture queue is empty')
q['currentIndex'] = 0
q['currentEntryId'] = q['entries'][0]['queueEntryId']
q['positionMs'] = 0
q['revision'] = max(int(revision), int(q.get('revision', 0))) + 1
target.execute('UPDATE queue_snapshot SET revision=?, metadata=? WHERE id=1', (q['revision'], json.dumps(q, ensure_ascii=False, separators=(',', ':'))))
row = target.execute('SELECT metadata FROM local_settings WHERE id=1').fetchone()
if row is None: raise RuntimeError('fixture settings missing')
s = json.loads(row[0]); s['closeToTray'] = False
s.setdefault('desktop', {})['notifyTrackChanges'] = False
target.execute('UPDATE local_settings SET metadata=? WHERE id=1', (json.dumps(s, ensure_ascii=False, separators=(',', ':')),))
target.commit()
if target.execute('PRAGMA integrity_check').fetchone()[0] != 'ok': raise RuntimeError('copied fixture integrity failed')
print(json.dumps({'entries': len(q['entries']), 'firstSongId': q['entries'][0]['track']['neteaseSongId'], 'revision': q['revision']}))
target.close(); source.close()
'@
$fixtureInfo=$python | python - $dbSource $dbTarget | ConvertFrom-Json
foreach($name in @('audio','resources')){
 $sourceManifest=Get-FileHash (Join-Path $source "cache/$name/manifest.json") -Algorithm SHA256
 $copyManifest=Get-FileHash (Join-Path $profile "cache/$name/manifest.json") -Algorithm SHA256
 if($sourceManifest.Hash -ne $copyManifest.Hash){throw "Copied $name cache manifest differs from fixture source"}
}
$audioManifest=Get-Content (Join-Path $profile 'cache/audio/manifest.json') -Raw|ConvertFrom-Json -AsHashtable
if($audioManifest.Count -lt 2){throw 'Native fixture needs at least two cached audio entries for next-track acceptance'}
foreach($entry in $audioManifest.Values){
 $blob=Join-Path $profile "cache/audio/$($entry.Key).blob"
 if(-not(Test-Path $blob)){throw "Cached audio blob missing: $($entry.Key)"}
 $item=Get-Item $blob;if($item.Length -ne $entry.Bytes){throw "Cached audio blob size mismatch: $($entry.Key)"}
 if((Get-FileHash $blob -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entry.SHA256.ToLowerInvariant()){throw "Cached audio blob SHA256 mismatch: $($entry.Key)"}
}
$resourceManifest=Get-Content (Join-Path $profile 'cache/resources/manifest.json') -Raw|ConvertFrom-Json
foreach($entry in $resourceManifest){
 $blob=Join-Path $profile "cache/resources/$($entry.key).bin"
 if(-not(Test-Path $blob)){throw "Cached resource blob missing: $($entry.key)"}
 if((Get-Item $blob).Length -ne $entry.size){throw "Cached resource blob size mismatch: $($entry.key)"}
}
$reportPath=Join-Path $output "native-player-selectors-$stamp-report.json"
$stdout=Join-Path $profile 'stdout.log';$stderr=Join-Path $profile 'stderr.log'
$saved=@{}
foreach($n in @('OTONEIGO_DATA_DIR','HTTP_PROXY','HTTPS_PROXY')){$saved[$n]=[Environment]::GetEnvironmentVariable($n,'Process')}
$native=$null
try{
 $env:OTONEIGO_DATA_DIR=$profile;$env:HTTP_PROXY='http://127.0.0.1:1';$env:HTTPS_PROXY='http://127.0.0.1:1'
 $native=Start-Process -FilePath $exe -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdout -RedirectStandardError $stderr
 $null=$native.Handle
 $native.Id|Set-Content (Join-Path $profile 'owned-process-id.txt')
}finally{foreach($n in $saved.Keys){[Environment]::SetEnvironmentVariable($n,$saved[$n],'Process')}}
try{
 & node (Join-Path $PSScriptRoot 'native-player-selectors-acceptance.mjs') $profile $fixtureInfo.firstSongId $reportPath
 $nodeExit=$LASTEXITCODE
 if($nodeExit -ne 0){throw "Native player selector acceptance failed with node exit $nodeExit"}
 if(-not $native.WaitForExit(20000)){throw 'Native acceptance window did not close within 20 seconds'}
 $native.Refresh();if($native.ExitCode -ne 0){throw "Native app exited with code $($native.ExitCode)"}
 $integrity=@'
import sqlite3,sys
c=sqlite3.connect(sys.argv[1]); print(c.execute('PRAGMA integrity_check').fetchone()[0]); c.close()
'@ | python - $dbTarget
 if($integrity.Trim() -ne 'ok'){throw "Native copied DB integrity check failed: $integrity"}
 $persisted=@'
import json,sqlite3,sys
c=sqlite3.connect(sys.argv[1]); q=json.loads(c.execute('SELECT metadata FROM queue_snapshot WHERE id=1').fetchone()[0]); s=json.loads(c.execute('SELECT metadata FROM local_settings WHERE id=1').fetchone()[0]);
assert q['currentIndex']==1 and q['currentEntryId']==q['entries'][1]['queueEntryId'] and q['positionMs']>0, 'queue position/index not persisted after native close'
assert s['closeToTray'] is False and s['regionMode']=='auto', 'settings not persisted after native close'
print(json.dumps({'currentIndex':q['currentIndex'],'positionMs':q['positionMs'],'currentSongId':q['entries'][q['currentIndex']]['track']['neteaseSongId'],'regionMode':s['regionMode']})); c.close()
'@ | python - $dbTarget | ConvertFrom-Json
 if($LASTEXITCODE -ne 0 -or $null -eq $persisted){throw 'Persisted native queue/settings inspection failed'}
 $privacyCheck=@'
import json,sqlite3,sys
c=sqlite3.connect(sys.argv[1])
for table in ('queue_snapshot','local_favorites','local_history'):
 try: rows=c.execute(f'SELECT metadata FROM {table}').fetchall()
 except sqlite3.Error: continue
 for (raw,) in rows:
  value=json.loads(raw)
  def walk(x):
   if isinstance(x,dict):
    for k,v in x.items():
     if k.lower() in ('url','mediaurl','cookie','token','access_token','refresh_token'):
      if k.lower() != 'coverref' and v: raise RuntimeError(f'persisted sensitive field: {table}.{k}')
     walk(v)
   elif isinstance(x,list):
    for v in x: walk(v)
  walk(value)
print('clean')
'@ | python - $dbTarget
 if($privacyCheck.Trim() -ne 'clean'){throw "Privacy scan failed: $privacyCheck"}
 $sourceUntouched=((Get-FileHash $dbSource -Algorithm SHA256).Hash -eq $sourceHashes['library.db'])
 foreach($file in (Get-ChildItem (Join-Path $source 'cache') -File -Recurse)){$relative=$file.FullName.Substring((Join-Path $source 'cache').Length+1);if((Get-FileHash $file.FullName -Algorithm SHA256).Hash -ne $sourceHashes["cache/$relative"]){$sourceUntouched=$false}}
 if(-not $sourceUntouched){throw 'Read-only source profile changed during acceptance'}
 $acceptanceHash=(Get-FileHash $exe -Algorithm SHA256).Hash
 $hostReport=[ordered]@{pass=$true;nativeExitCode=$native.ExitCode;profile=$profile;sourceProfile=$source;sourceProfileUntouched=$sourceUntouched;integrity=$integrity.Trim();privacy='clean';acceptanceExeSha256=$acceptanceHash;persistedQueue=[ordered]@{currentIndex=$persisted.currentIndex;positionMs=$persisted.positionMs;currentSongId=$persisted.currentSongId;regionMode=$persisted.regionMode};acceptanceReport=$reportPath}
 $hostReportPath=$reportPath -replace '-report\.json$','-host-report.json'
 $hostReport|ConvertTo-Json -Depth 8|Set-Content $hostReportPath -Encoding utf8
 Write-Output "Fixture: $profile; copied rows: $($fixtureInfo.entries); persisted song: $($persisted.currentSongId) at $($persisted.positionMs) ms; native process exit: $($native.ExitCode); report: $reportPath"
}finally{
 if($null -ne $native){
  try{if(-not $native.HasExited){$native.Kill();$null=$native.WaitForExit(10000)}}finally{$native.Dispose()}
 }
}
