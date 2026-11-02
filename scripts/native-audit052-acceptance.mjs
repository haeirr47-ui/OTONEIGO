// Actual native UI/SQLite/media, process-only offline proxy. No screenshots or RPC mocks.
import assert from 'node:assert/strict';
import {readFile,writeFile,open} from 'node:fs/promises';
import {nativeHarness,until,wait} from './native-harness.mjs';
const mode=process.argv[2];assert.ok(['recovery','indexed','unknown','lazy'].includes(mode));
const profile='output/native-audit-052-'+(mode==='indexed'?'recovery':mode);
const h=await nativeHarness(),{page,call}=h;
const report={mode,nativeWebView2:true,screenshots:false,rpcInterception:false,mediaInterception:false,phase:'recovery-status'};
try{
 await until(async()=>(await call('GetDesktopSnapshot')).ready);
 assert.equal((await call('GetDesktopSnapshot')).playback,'paused');
 let s=await call('GetStorageRecoveryStatus');
 if(mode==='lazy'){
  s=await until(async()=>{const v=await call('GetStorageRecoveryStatus');return v.quarantinedFavorites===1&&v.quarantinedHistory===1&&v;});
  assert.equal(s.settingsRecovered,false);
  await page.locator('.storage-recovery-notice').filter({hasText:'隔离保留'}).waitFor();
  await page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name:'本地收藏',exact:true}).click();
  await until(async()=>await page.locator('main .music-card').count()===200);
  await until(async()=>!(await page.locator('main .music-card button.favorite').first().isDisabled()));
  assert.ok(!(await call('GetLocalFavoriteIDs')).includes('33894312'));
  await until(async()=>!(await call('GetDesktopSnapshot')).favorite);
  report.currentSchemaLazyRecovery=true;report.quarantinedTrackIsNotFavorite=true;
 }else if(mode==='unknown'){
  assert.deepEqual(s,{quarantinedFavorites:0,quarantinedHistory:0,settingsRecovered:false});
  const settings=await call('GetSettings');assert.equal(settings.preferredQuality,'exhigh');
  await call('SetSettings',{...settings,volume:.19});assert.equal((await call('GetSettings')).volume,.19);
  report.effectiveQualityRemains320=true;report.unknownSettingsSaved=true;
 }else{
  assert.equal(s.quarantinedFavorites,1);assert.equal(s.quarantinedHistory,1);assert.equal(s.settingsRecovered,true);
  await page.locator('.storage-recovery-notice').filter({hasText:'隔离保留'}).waitFor();
  assert.equal((await call('GetSettings')).gdEnabled,false);
  assert.equal((await call('GetLocalFavoriteIDs')).length,1004);
  report.badRowsDidNotBlockStartup=true;report.warningVisible=true;
 }
 const queue=await call('LoadQueue');
 if(mode!=='lazy'){
  await assert.rejects(call('SetLocalFavorite',queue.entries[0].track,true),/LOCAL_METADATA_REQUIRES_NEWER_VERSION/);
  report.futureFavoriteNotOverwritten=true;
 }
 if(mode==='recovery'||mode==='indexed'){
  report.phase='actual-cached-playback';
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__audit052Players=new Set();HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__audit052Audio=this;window.__audit052Players.add(this);return play.apply(this,args)};});
  const start=Date.now();await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.waitForFunction(position=>window.__audit052Audio&&!window.__audit052Audio.paused&&window.__audit052Audio.currentTime>position/1000+.6,queue.positionMs,{timeout:30000});
  const media=await page.evaluate(()=>({players:window.__audit052Players.size,duration:window.__audit052Audio.duration,time:window.__audit052Audio.currentTime}));
  assert.equal(media.players,1);assert.ok(Math.abs(media.duration-267.232)<2);await until(async()=>(await call('GetDesktopSnapshot')).cacheHit);
  report.cachedPlayback={...media,startupWallMs:Date.now()-start};
  await page.getByRole('slider',{name:'播放进度',exact:true}).fill('81000');
  await page.waitForFunction(()=>Math.abs(window.__audit052Audio.currentTime-81)<.5,null,{timeout:10000});
  await page.getByRole('button',{name:'暂停',exact:true}).click();
  const entries=JSON.parse(await readFile(profile+'/cache/audio/manifest.json','utf8'));
  const entry=Object.values(entries).find(e=>e.SongID==='33894312');assert.ok(entry.Chunks.length>0);report.integrityChunks=entry.Chunks.length;
  report.legacyUpgradedOrIndexed=true;
  if(mode==='indexed'){
   report.phase='corruption-range-and-explicit-clear';
   // Pause releases the player's lease. Acquire a separate bounded read lease
   // for the real HTTP probe; the capability stays in Node memory only.
   const lease=await call('ResolveStream','33894312','exhigh');assert.ok(lease.cacheHit);
   const file=await open(profile+'/cache/audio/'+entry.Key+'.blob','r+');
   try{const b=Buffer.alloc(1);await file.read(b,0,1,6*1024*1024);b[0]^=255;await file.write(b,0,1,6*1024*1024);}finally{await file.close();}
   report.corruptionHttpStatuses=[];
   try{for(let i=0;i<2;i++){const r=await fetch(lease.url,{headers:{Range:'bytes=6291456-7340031'}});report.corruptionHttpStatuses.push(r.status);assert.equal(r.status,410);await r.arrayBuffer();}}
   finally{await call('ReleasePlaybackSource',lease.cacheToken);}
   report.corruptDemandedChunkRejected410=true;
   await page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name:'播放历史',exact:true}).click();
   await page.getByRole('button',{name:'清空历史',exact:true}).click();
   await until(async()=>(await call('ListLocalHistory',1,0)).length===0);
   const after=await call('GetStorageRecoveryStatus');assert.equal(after.quarantinedHistory,0);assert.equal(after.quarantinedFavorites,1);assert.equal(after.settingsRecovered,true);
   report.manualHistoryClearIncludesQuarantine=true;
  }
 }
 assert.ok((await call('GetResourceCacheStats')).cachedCovers>=1000);report.startupWith1000LargeCovers=true;
 report.phase='complete';report.pass=true;
}catch(e){report.pass=false;report.failure=e instanceof assert.AssertionError?'ASSERTION_FAILED':'NATIVE_CHECK_FAILED';report.failureKind=/strict mode violation/.test(e.message)?'AMBIGUOUS_TEST_LOCATOR':e.name;report.location=e.stack?.match(/native-audit052-acceptance\.mjs:\d+:\d+/)?.[0]??'HELPER';}
finally{await writeFile(`output/playwright/native-audit052-${mode}-report.json`,JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await h.browser.close();if(!report.pass)process.exitCode=1;}
