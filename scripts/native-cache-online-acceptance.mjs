import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {nativeHarness,until,wait} from './native-harness.mjs';
const h=await nativeHarness(),{page,call}=h;
const report={native:true,screenshots:false,networkInterception:false,controlledMediaReadDelayMs:80};
const stats=()=>call('GetAudioCacheStats');
async function stop(){const q=await call('LoadQueue');const b=page.getByRole('button',{name:'暂停',exact:true});if(await b.count()){await b.click();await until(async()=>(await call('LoadQueue')).revision>q.revision);}}
const musicRequests=async()=>{try{return(await readFile('output/native-cache-desktop/log/playback.jsonl','utf8')).trim().split('\n').map(l=>JSON.parse(l)).filter(e=>['normal-api','compatible-api','gd-api'].includes(e.stage)).length;}catch{return 0;}};
try{
  const s=await call('GetSettings');assert.equal(s.audioCache.enabled,true);assert.equal(s.audioCache.maxBytes,20*1024**3);assert.equal(s.preferredQuality,'exhigh');assert.equal(s.gdEnabled,true);assert.equal(s.volume,0.37);
  assert.deepEqual((await call('GetLocalFavorites')).map(t=>t.neteaseSongId),['1833299189','33894312']);assert.equal((await call('GetLocalPlaylist','migration-seed')).tracks.length,2);
  assert.ok((await call('ListLocalHistory',2,5000)).length>0);report.schema3DataPreserved=true;report.historyBeyond5000=true;
  await stop();await call('ClearAudioCache');await until(async()=>(await stats()).usedBytes===0);
  const old=await call('LoadQueue');await call('SaveQueue',{...old,entries:old.entries.map((e,i)=>({...e,queueEntryId:`cache-check-${i}`})),currentEntryId:'cache-check-0',currentIndex:0,positionMs:0,revision:old.revision+1});
  await h.reload();
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__cachePlayers=new Set();window.__cachePlaying=[];HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__cacheAudio=this;window.__cachePlayers.add(this);if(!this.__cacheObserved){this.__cacheObserved=true;this.addEventListener('playing',()=>window.__cachePlaying.push(Date.now()));}return play.apply(this,args);};});
  const start=Date.now();await page.getByRole('button',{name:'播放',exact:true}).click();await page.waitForFunction(()=>window.__cacheAudio?.currentTime>0.5&&!window.__cacheAudio.paused,undefined,{timeout:30000});
  report.firstPlayWallMs=Date.now()-start;report.firstPlayingStats=await stats();assert.equal(report.firstPlayingStats.cachedTracks,0);assert.equal(report.firstPlayingStats.activeDownloads,1);assert.ok(report.firstPlayingStats.usedBytes>0);report.progressiveBeforeEOF=true;
  await until(async()=>(await stats()).cachedTracks===1,60000);report.completedStats=await stats();assert.equal(report.completedStats.activeDownloads,0);assert.ok(report.completedStats.usedBytes>8*1024*1024);report.completeFileCommitted=true;
  const uri=await page.evaluate(()=>window.__cacheAudio.currentSrc);assert.equal(new URL(uri).hostname,'127.0.0.1');report.loopbackMediaOnly=true;
  await page.getByRole('slider',{name:'播放进度',exact:true}).fill('81000');await page.waitForFunction(()=>window.__cacheAudio.currentTime>=81&&window.__cacheAudio.currentTime<85);report.actualRangeSeek=true;
  await stop();const before=await musicRequests();const cachedStart=Date.now();await page.getByRole('button',{name:'播放',exact:true}).click();await page.waitForFunction(()=>window.__cacheAudio?.currentTime>=81&&!window.__cacheAudio.paused,undefined,{timeout:10000});report.cachedResumeWallMs=Date.now()-cachedStart;
  report.cachedLabel=await page.locator('footer .stream-source').textContent();assert.match(report.cachedLabel,/本地缓存/);assert.equal(await musicRequests(),before);assert.equal((await stats()).activeDownloads,0);report.warmMusicRequestDelta=0;
  assert.equal(await page.evaluate(()=>window.__cachePlayers.size),1);report.singleAudio=true;
  const nav=name=>page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name,exact:true});
  await nav('播放历史').click();const count=await page.locator('main .music-card').count();assert.ok(count>=200);await page.getByRole('button',{name:'加载更多',exact:true}).click();await until(async()=>await page.locator('main .music-card').count()>=count+200);report.cursorHistoryPaging=true;
  await page.locator('footer').getByRole('button',{name:'收藏：OTONEIGO 缓存原生验收',exact:true}).click();await until(async()=>(await call('GetLocalFavorites'))[0]?.neteaseSongId==='1850977722');report.newestFavoriteFirst=true;
  await nav('设置').click();await page.getByLabel('关闭窗口时隐藏到托盘').check();await until(async()=>(await call('GetSettings')).closeToTray===true);
  await call('MinimizeToTray');const t=await page.evaluate(()=>window.__cacheAudio.currentTime);await wait(1500);assert.equal(await page.evaluate(()=>window.__cacheAudio.paused),false);assert.ok(await page.evaluate(()=>window.__cacheAudio.currentTime)>t+0.8);await call('ShowMainWindow');report.trayHideRestoreContinuesAudio=true;
  await page.getByLabel('关闭窗口时隐藏到托盘').uncheck();await until(async()=>(await call('GetSettings')).closeToTray===false);
  report.mediaSession=await page.evaluate(()=>({available:!!navigator.mediaSession,title:navigator.mediaSession?.metadata?.title,playbackState:navigator.mediaSession?.playbackState}));
  report.pass=true;console.log(JSON.stringify(report,null,2));
}catch(error){report.pass=false;report.failure=error.name;report.alerts=await page.getByRole('alert').allTextContents();console.log(JSON.stringify(report,null,2));throw error;
}finally{await writeFile('output/playwright/cache-online-native-report.json',JSON.stringify(report,null,2));await h.browser.close();}
