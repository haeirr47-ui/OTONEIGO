// Actual native windows and service. No network/stream/RPC interception, no screenshots.
import assert from 'node:assert/strict';
import {mkdir,writeFile,readFile} from 'node:fs/promises';
import {nativeHarness,until,wait} from './native-harness.mjs';
const h=await nativeHarness(),{page,call}=h;
const report={nativeWebView2:true,networkInterception:false,screenshots:false,muted:true,phase:'setup'};
const output='output/playwright/native-desktop05-report.json';
async function runtime(p){await p.evaluate(async()=>{const dispatch=window._wails.dispatchWailsEvent;window.__nativeTestRuntime=await import('/wails/runtime.js');window._wails.dispatchWailsEvent=dispatch;});}
const snapshot=()=>call('GetDesktopSnapshot');
async function ready(){return until(async()=>{const s=await snapshot();return s.ready&&s;},15000);}
const command=async(command,value=0,songId)=>{const s=await snapshot();return call('DispatchDesktopCommand',{epoch:s.epoch,command,songId:songId??s.songId,value});};
async function save(){await mkdir('output/playwright',{recursive:true});await writeFile(output,JSON.stringify(report,null,2));}
try{
 const settings=await call('GetSettings');
 assert.equal(settings.preferredQuality,'exhigh');assert.equal(settings.desktop.notifyTrackChanges,false);
 assert.equal((await call('LoadQueue')).entries[0].queueEntryId,'desktop05-a');
 await call('SetSettings',{...settings,desktop:{...settings.desktop,notifyTrackChanges:true}});
 const context=page.context();
 await context.addInitScript(()=>{
   window.__desktopAudioCount=0;window.__desktopPlayed=new Set();window.__desktopEnded=[];
   const AudioOriginal=window.Audio;
   window.Audio=new Proxy(AudioOriginal,{construct(target,args,newTarget){window.__desktopAudioCount++;return Reflect.construct(target,args,newTarget);}});
   const play=HTMLMediaElement.prototype.play;
   HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__desktopAudio=this;window.__desktopPlayed.add(this);
     if(!this.__desktopObserved){this.__desktopObserved=true;this.addEventListener('ended',()=>window.__desktopEnded.push({duration:this.duration,time:this.currentTime}));}
     return play.apply(this,args);
   };
 });
 await h.reload();await ready();
 await call('ShowDesktopView','mini');await call('ShowDesktopView','lyrics');
 const find=kind=>until(()=>context.pages().find(p=>new URL(p.url()).searchParams.get('view')===kind));
 const mini=await find('mini'),lyrics=await find('lyrics');
 await mini.reload();await lyrics.reload();
 await mini.getByRole('main',{name:'迷你播放器',exact:true}).waitFor();
 await lyrics.getByRole('main',{name:'桌面歌词',exact:true}).waitFor();
 await runtime(mini);await runtime(lyrics);
 report.phase='one-executor';
 assert.equal(await page.evaluate(()=>window.__desktopAudioCount),1);
 assert.equal(await mini.evaluate(()=>window.__desktopAudioCount),0);
 assert.equal(await lyrics.evaluate(()=>window.__desktopAudioCount),0);
 report.audioConstruction={main:1,mini:0,lyrics:0};
 await mini.getByRole('button',{name:'播放',exact:true}).click();
 await until(async()=>{const s=await snapshot();return s.playback==='playing'&&s.positionMs>800;},40000);
 const direct=await snapshot();assert.equal(direct.provider,'netease-direct');
 const media=await page.evaluate(()=>({duration:window.__desktopAudio.duration,players:window.__desktopPlayed.size}));
 assert.equal(media.players,1);assert.ok(Math.abs(media.duration-267.232)<2);
 report.direct={provider:direct.provider,quality:direct.quality,durationSeconds:media.duration};
 await mini.getByRole('button',{name:'暂停',exact:true}).click();
 await until(async()=>(await snapshot()).playback==='paused');
 await until(async()=>(await call('GetDesktopNativeState')).taskbarState==='paused');
 await command('seek',81000);await until(async()=>Math.abs((await snapshot()).positionMs-81000)<1500);
 await command('volume',.17);await until(async()=>Math.abs((await snapshot()).volume-.17)<.001);
 const wasFavorite=(await snapshot()).favorite;await command('toggle-favorite');
 await until(async()=>(await snapshot()).favorite!==wasFavorite);await command('toggle-favorite');
 await until(async()=>(await snapshot()).favorite===wasFavorite);
 const old=await snapshot();await assert.rejects(call('DispatchDesktopCommand',{epoch:old.epoch-1,command:'seek',songId:old.songId,value:0}));
 await assert.rejects(command('seek',0,'999999999999'));
 report.controls={pause:true,seekBeyondPreview:true,volume:true,favorite:true,staleEpochRejected:true,staleTrackRejected:true};
 report.phase='observer-lifecycle';
 await mini.evaluate(()=>window.__nativeTestRuntime.Window.SetPosition(85,95));
 await lyrics.evaluate(()=>window.__nativeTestRuntime.Window.SetPosition(105,305));
 await wait(700);
 const placed=await call('GetSettings');assert.ok(placed.desktop.miniPlacement.valid);assert.ok(placed.desktop.lyricsPlacement.valid);
 report.savedPlacements={mini:placed.desktop.miniPlacement,lyrics:placed.desktop.lyricsPlacement};
 await mini.getByRole('button',{name:'隐藏迷你播放器'}).click();
 await until(async()=>!(await call('GetDesktopNativeState')).miniVisible);
 await call('ShowDesktopView','mini');await until(async()=>(await call('GetDesktopNativeState')).miniVisible);
 assert.equal(context.pages().filter(p=>p.url().includes('?view=mini')).length,1);
 await command('toggle-lyrics-lock');await until(async()=>(await call('GetDesktopNativeState')).lyricsLocked);
 await command('toggle-lyrics-lock');await until(async()=>!(await call('GetDesktopNativeState')).lyricsLocked);
 report.hideReopenUsesSameWindow=true;report.lockUnlock=true;
 const epoch=(await snapshot()).epoch;await h.reload();await ready();
 assert.ok((await snapshot()).epoch>epoch);assert.equal((await snapshot()).playback,'paused');
 await mini.getByRole('button',{name:'播放',exact:true}).waitFor();
 report.mainReloadRestoresPaused=true;
 report.phase='background-clock-and-notification';
 await command('play');await until(async()=>(await snapshot()).playback==='playing',40000);
 const notifyBefore=await call('GetDesktopNativeState');assert.equal(notifyBefore.notificationRequests,0);
 await call('MinimizeToTray');await until(async()=>!(await call('GetDesktopNativeState')).mainVisible);
 const start=(await snapshot()).positionMs;await wait(1500);assert.ok((await snapshot()).positionMs>start+600);
 report.hiddenMainClockContinues=true;
 await mini.getByRole('button',{name:'下一首',exact:true}).click();
 await until(async()=>{const s=await snapshot();return s.songId==='1850977722'&&s.playback==='playing'&&s.positionMs>500;},45000);
 const gd=await snapshot();assert.equal(gd.provider,'gd-netease');
 await until(async()=>(await call('GetDesktopNativeState')).notificationRequests===1);
 const native=await call('GetDesktopNativeState');assert.equal(native.notificationAccepted,1);assert.equal(native.taskbarState,'normal');
 await wait(1300);assert.equal((await call('GetDesktopNativeState')).notificationRequests,1);
 await command('pause');await command('play');await until(async()=>(await snapshot()).playback==='playing');
 assert.equal((await call('GetDesktopNativeState')).notificationRequests,1);
 await until(async()=>!!(await snapshot()).currentLyric,15000);
 const lyric=await snapshot();
 assert.equal(await lyrics.locator('.desktop-current-lyric').textContent(),lyric.currentLyric);
 report.lyricsProjectedFromMain=true;
 report.gd={provider:gd.provider,quality:gd.quality,nativeDurationSeconds:await page.evaluate(()=>window.__desktopAudio.duration)};
 report.notifications={requests:native.notificationRequests,apiAccepted:native.notificationAccepted,visualDisplayNotAsserted:true,resumeDoesNotRepeat:true};
 report.taskbar={available:native.taskbarAvailable,state:native.taskbarState,durationMs:native.taskbarDurationMs};
 const log=(await readFile('output/native-desktop-05/log/playback.jsonl','utf8')).split('\n').filter(Boolean).map(x=>JSON.parse(x));
 report.requestStages=Object.fromEntries(['normal-api','compatible-api','gd-api','cache-hit'].map(stage=>[stage,log.filter(e=>e.stage===stage).length]));
 assert.equal(report.requestStages['normal-api'],0);assert.ok(report.requestStages['compatible-api']>0);assert.equal(report.requestStages['gd-api'],1);
 report.phase='natural-end-observation';report.shortChecksPassed=true;await save();
 // Leave actual single media running. Poll from Node, with no independent audio clock.
 const end=Date.now()+255000;
 while(Date.now()<end){
   const state=await page.evaluate(()=>({ended:window.__desktopEnded.length,players:window.__desktopPlayed.size,time:window.__desktopAudio.currentTime}));
   if(state.ended>0){report.naturalEnd={count:state.ended,oneExecutor:state.players===1};break;}
   await wait(1000);
 }
 assert.ok(report.naturalEnd,'GD actual song reached natural end');
 await until(async()=>(await snapshot()).songId==='33894312',15000);
 await command('pause');await call('ShowMainWindow');
 report.autoNextSameExecutor=true;report.pass=true;report.phase='complete';
}catch(error){report.pass=false;report.failure=error instanceof assert.AssertionError?'ASSERTION_FAILED':'NATIVE_CHECK_FAILED';report.location=error.stack?.match(/native-desktop05-acceptance\.mjs:\d+:\d+/)?.[0]??'HELPER';console.error(report.failure,report.phase,report.location);}
finally{await save();console.log(JSON.stringify(report,null,2));await h.browser.close();if(!report.pass)process.exitCode=1;}
