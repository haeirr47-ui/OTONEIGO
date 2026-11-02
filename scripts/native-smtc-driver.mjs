import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {writeFile} from 'node:fs/promises';
import {nativeHarness,wait} from './native-harness.mjs';
const h=await nativeHarness(),{page,call}=h;
const report={nativeWindowsSMTC:true,networkInterception:false,isolatedProfile:'output/native-cache-desktop',controlledMediaReadDelayMs:80};
try{
  const q=await call('LoadQueue');assert.ok(q.entries[0]?.track.name.startsWith('OTONEIGO 缓存原生验收'));
  await call('SaveQueue',{...q,currentIndex:0,currentEntryId:q.entries[0].queueEntryId,positionMs:0,revision:q.revision+1});await h.reload();
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__smtcPlayers=new Set();HTMLMediaElement.prototype.play=function(...args){window.__smtcAudio=this;window.__smtcPlayers.add(this);this.muted=false;this.volume=.01;return play.apply(this,args);};});
  await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.waitForFunction(()=>window.__smtcAudio?.currentTime>.5&&!window.__smtcAudio.paused,undefined,{timeout:30000});
  report.beforeSystemControls=await call('GetAudioCacheStats');assert.equal(report.beforeSystemControls.cachedTracks,0);assert.equal(report.beforeSystemControls.activeDownloads,1);
  let pauseStats;
  await page.exposeFunction('__observeSystemPause',async()=>{pauseStats??=await call('GetAudioCacheStats');});
  await page.evaluate(()=>window.__smtcAudio.addEventListener('pause',()=>void window.__observeSystemPause(),{once:true}));
  await wait(500);
  try{
    const r=await promisify(execFile)('powershell.exe',['-NoProfile','-File','scripts/native-smtc-acceptance.ps1','-Control'],{windowsHide:true});
    report.windowsResults=r.stdout.trim().split(/\r?\n/).map(JSON.parse);
  }catch(e){report.windowsResults=(e.stdout??'').trim().split(/\r?\n/).filter(Boolean).map(JSON.parse);throw Error('NATIVE_SMTC_CONTROL_FAILED');}
  assert.equal(report.windowsResults.at(-1)?.outcome,'pass');
  report.pauseStats=pauseStats;assert.equal(pauseStats?.cachedTracks,0);report.pausedBeforeCompletedCache=true;
  await page.waitForFunction(()=>navigator.mediaSession?.metadata?.title==='OTONEIGO 缓存原生验收 · 下一首'&&!window.__smtcAudio.paused&&window.__smtcAudio.currentTime>.5,undefined,{timeout:15000});
  report.actualNextQueueEntry=(await call('LoadQueue')).currentEntryId;assert.equal(report.actualNextQueueEntry,q.entries[1].queueEntryId);
  assert.equal(await page.evaluate(()=>window.__smtcPlayers.size),1);report.singleAudio=true;report.pass=true;
}catch(e){report.pass=false;report.failure=e.name;throw e;}
finally{
  await page.evaluate(()=>{if(window.__smtcAudio)window.__smtcAudio.muted=true;}).catch(()=>{});
  const pause=page.getByRole('button',{name:'暂停',exact:true});if(await pause.count())await pause.click();
  await writeFile('output/playwright/native-smtc-final-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await h.browser.close();
}
