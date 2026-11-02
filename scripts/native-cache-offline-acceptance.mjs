import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {nativeHarness,until,wait} from './native-harness.mjs';
const h=await nativeHarness(),{page,call}=h;
const report={native:true,isolatedProfile:'output/native-cache-desktop',processOnlyOfflineProxy:true,networkInterception:false};
const stats=()=>call('GetAudioCacheStats');
const musicRequests=async()=>{try{return(await readFile('output/native-cache-desktop/log/playback.jsonl','utf8')).trim().split('\n').map(JSON.parse).filter(e=>['normal-api','compatible-api','gd-api'].includes(e.stage)).length;}catch{return 0;}};
try{
  assert.equal((await call('GetLocalFavorites'))[0].neteaseSongId,'1850977722');
  assert.ok((await call('ListLocalHistory',2,5000)).length>0);
  assert.equal((await call('GetLocalPlaylist','migration-seed')).tracks.length,2);
  assert.equal((await call('LoadQueue')).entries.length,2);
  report.persistedLibrary=true;
  assert.equal(await page.getByRole('button',{name:'播放',exact:true}).count(),1);
  assert.equal((await stats()).cachedTracks,1);report.restoredPaused=true;
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__offlinePlayers=new Set();HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__offlineAudio=this;window.__offlinePlayers.add(this);return play.apply(this,args);};});
  const before=await musicRequests(),start=Date.now();
  await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.waitForFunction(()=>window.__offlineAudio&&!window.__offlineAudio.paused&&window.__offlineAudio.currentTime>0.5,undefined,{timeout:10000});
  report.startupWallMs=Date.now()-start;
  assert.match(await page.locator('footer .stream-source').textContent(),/本地缓存/);
  assert.equal(await musicRequests(),before);report.audioRequestDelta=0;
  assert.equal(new URL(await page.evaluate(()=>window.__offlineAudio.currentSrc)).hostname,'127.0.0.1');
  assert.equal(await page.evaluate(()=>window.__offlinePlayers.size),1);report.singleAudio=true;
  await page.getByRole('slider',{name:'播放进度',exact:true}).fill('91000');
  await page.waitForFunction(()=>window.__offlineAudio.currentTime>=91&&window.__offlineAudio.currentTime<95);report.offlineRangeSeek=true;
  const settings=await call('GetSettings');
  await call('SetSettings',{...settings,audioCache:{...settings.audioCache,enabled:false}});
  let t=await page.evaluate(()=>window.__offlineAudio.currentTime);await wait(1000);
  assert.ok(await page.evaluate(()=>!window.__offlineAudio.paused&&window.__offlineAudio.currentTime)>t);report.disablingCachePreservesPlayback=true;
  await call('SetSettings',settings);
  report.clearResult=await call('ClearAudioCache');
  assert.equal(report.clearResult.retainedActiveTracks,1);
  t=await page.evaluate(()=>window.__offlineAudio.currentTime);await wait(1200);
  assert.ok(await page.evaluate(()=>!window.__offlineAudio.paused&&window.__offlineAudio.currentTime)>t);report.clearPreservesPlayback=true;
  await page.getByRole('button',{name:'暂停',exact:true}).click();
  report.afterPause=await stats();
  // Ordinary pause now retains loaded metadata for the Windows media session.
  // An outstanding browser response can pin the cleared file until source reset.
  if(report.afterPause.usedBytes>0){report.pausedSourceRetainsActivePin=true;await h.reload();}
  await until(async()=>(await stats()).usedBytes===0,10000);
  report.afterRelease=await stats();assert.equal(report.afterRelease.cachedTracks,0);report.deletedAfterSourceReset=true;
  report.pass=true;console.log(JSON.stringify(report,null,2));
}catch(e){report.pass=false;report.failure=e.name;throw e;}
finally{await writeFile('output/playwright/cache-offline-native-report.json',JSON.stringify(report,null,2));await h.browser.close();}
