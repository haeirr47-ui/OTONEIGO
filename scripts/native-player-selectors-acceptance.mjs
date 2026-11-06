// Native WebView2 player selector regression. Real Wails services and cached bytes;
// only HTMLMediaElement.play is muted and counted. No RPC or media interception.
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
import {nativeHarness,until,wait} from './native-harness.mjs';
import {verifyShortcuts} from './native-shortcuts-checks.mjs';

const profile=path.resolve(process.argv[2]??'');
if(!process.argv[2])throw new Error('Usage: native-player-selectors-acceptance.mjs <isolated-profile> <first-song-id>');
const expectedSong=process.argv[3];
const report={kind:'native-webview2-player-selector-regression',native:true,mediaInterception:false,rpcInterception:false,cacheFixture:true};
const h=await nativeHarness(),{page,call}=h;
const out=path.resolve(process.argv[4]??'output/playwright/native-player-selectors-report.json');
const nav=name=>page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name,exact:true});
const queue=()=>call('LoadQueue');
try{
  assert.equal((await queue()).entries[0].track.neteaseSongId,expectedSong,'prepared first queue row');
  assert.equal((await call('GetSettings')).closeToTray,false);
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__selectorPlayers=new Set();window.__selectorAudio=null;HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__selectorAudio=this;window.__selectorPlayers.add(this);return play.apply(this,args);};});
  await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.waitForFunction(()=>window.__selectorAudio?.currentTime>1&&!window.__selectorAudio.paused,undefined,{timeout:30000});
  const stream=await page.evaluate(()=>({src:new URL(window.__selectorAudio.currentSrc).hostname,duration:window.__selectorAudio.duration,time:window.__selectorAudio.currentTime}));
  assert.equal(stream.src,'127.0.0.1');assert.ok(stream.time>1);report.cachedNativePlayback=stream;
  const slider=page.getByRole('slider',{name:'播放进度',exact:true});
  await slider.fill('81000');await page.waitForFunction(()=>window.__selectorAudio.currentTime>=80&&window.__selectorAudio.currentTime<84,undefined,{timeout:10000});report.seek=true;
  await page.getByRole('button',{name:'暂停',exact:true}).click();await page.waitForFunction(()=>window.__selectorAudio.paused);
  const pausedAt=await page.evaluate(()=>window.__selectorAudio.currentTime);await wait(700);assert.ok(Math.abs(await page.evaluate(()=>window.__selectorAudio.currentTime)-pausedAt)<0.15);
  await page.getByRole('button',{name:'播放',exact:true}).click();await page.waitForFunction(t=>!window.__selectorAudio.paused&&window.__selectorAudio.currentTime>t+0.4,pausedAt);report.pauseResume=true;
  assert.equal(await page.evaluate(()=>window.__selectorPlayers.size),1);report.singleAudioExecutor=true;

  const id=(await queue()).currentEntryId;
  await page.getByRole('button',{name:'下一首',exact:true}).click();
  await until(async()=>{const q=await queue();return q.currentEntryId!==id&&q.positionMs>0;},30000);report.nextTrack=true;
  assert.equal(await page.evaluate(()=>window.__selectorPlayers.size),1,'track change retains the same audio executor');
  report.singleAudioAfterNext=true;
  await page.getByRole('button',{name:'暂停',exact:true}).click();
  const qNow=await queue(),track=qNow.entries[qNow.currentIndex].track,songId=track.neteaseSongId;
  const wasFavorite=(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===songId);
  await page.locator('footer').getByRole('button',{name:`${wasFavorite?'取消收藏':'收藏'}：${track.name}`,exact:true}).click();
  await until(async()=>(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===songId)===!wasFavorite);
  if(wasFavorite){
    await page.locator('footer').getByRole('button',{name:`收藏：${track.name}`,exact:true}).click();
    await until(async()=>(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===songId));
  }
  await nav('本地收藏').click();const favoriteRegion=page.getByRole('region',{name:'本地收藏'});
  const favoriteCard=favoriteRegion.locator('.music-card').filter({has:page.getByRole('button',{name:`播放 ${track.name} - ${track.artists.map(a=>a.name).join(' / ')}`,exact:true})});
  await favoriteCard.waitFor();assert.ok(await favoriteCard.evaluate(el=>el.classList.contains('is-active')),'current favorite song highlight');
  assert.ok(await favoriteCard.getByRole('button',{name:`取消收藏：${track.name}`,exact:true}).count());
  assert.equal(await page.locator('footer').getByRole('button',{name:`取消收藏：${track.name}`,exact:true}).getAttribute('aria-pressed'),'true');report.favoriteListHighlightAndFooterSync=true;
  await page.locator('footer').getByRole('button',{name:`取消收藏：${track.name}`,exact:true}).click();
  await until(async()=>(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===songId)===false);
  await page.locator('footer').getByRole('button',{name:`收藏：${track.name}`,exact:true}).click();
  await until(async()=>(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===songId));report.sameSongFavoriteSync=true;

  const queueOpener=page.locator('footer').getByRole('button',{name:'播放队列',exact:true});await queueOpener.click();
  const qdialog=page.getByRole('dialog',{name:'播放队列'});await qdialog.waitFor();
  assert.equal(await qdialog.evaluate(d=>d.contains(document.activeElement)),true,'queue receives focus');
  await page.keyboard.press('Tab');assert.equal(await qdialog.evaluate(d=>d.contains(document.activeElement)),true,'queue focus remains contained');
  assert.ok(await qdialog.getByRole('button',{name:`取消收藏：${track.name}`,exact:true}).count());
  await page.keyboard.press('Escape');await qdialog.waitFor({state:'detached'});
  assert.equal(await queueOpener.evaluate(el=>el===document.activeElement),true);report.queueOverlayFocusEscapeRestore=true;
  const lyricsOpener=page.locator('footer').getByRole('button',{name:'展开歌词',exact:true}).last();await lyricsOpener.click();
  const lyrics=page.getByRole('dialog',{name:'歌词视图'});await lyrics.waitFor();
  assert.equal(await lyrics.evaluate(d=>d.contains(document.activeElement)),true,'lyrics receives focus');
  await page.keyboard.press('Tab');assert.equal(await page.evaluate(()=>{const d=document.querySelector('[role="dialog"][aria-label="歌词视图"]');return d?.contains(document.activeElement)||document.querySelector('footer')?.contains(document.activeElement);}),true,'lyrics focus remains inside lyrics/footer scope');
  await page.keyboard.press('Escape');await lyrics.waitFor({state:'detached'});
  assert.equal(await lyricsOpener.evaluate(el=>el===document.activeElement),true);report.lyricsOverlayFocusEscapeRestore=true;
  await verifyShortcuts(h,report);
  await nav('设置').click();
  const settings=page.getByRole('region',{name:'设置'});await settings.waitFor();
  await settings.getByLabel('海外播放兼容').selectOption('off');
  await until(async()=>(await call('GetSettings')).regionMode==='off');
  await settings.getByLabel('海外播放兼容').selectOption('auto');
  await until(async()=>(await call('GetSettings')).regionMode==='auto');report.settingsResponsive=true;
  report.pass=true;
}catch(e){report.pass=false;report.failure=e.name;report.message=e.message;report.stack=e.stack;throw e;
}finally{
  report.finalQueue=await queue().catch(()=>null);report.finalSettings=await call('GetSettings').catch(()=>null);
  await mkdir(path.dirname(out),{recursive:true});await writeFile(out,JSON.stringify(report,null,2));
  try{await page.evaluate(()=>window.__nativeTestRuntime.Window.Close());}catch(e){if(!/closed|Target|destroyed/.test(e.message))throw e;}
  await h.browser.close();
}
