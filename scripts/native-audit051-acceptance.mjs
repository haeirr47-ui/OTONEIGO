// Real native UI+SQLite. Only one initial library IPC failure is injected;
// playback, cache, subsequent RPCs and media remain real. No screenshots.
import assert from 'node:assert/strict';
import {writeFile} from 'node:fs/promises';
import {nativeHarness,until,wait} from './native-harness.mjs';
const h=await nativeHarness(),{page,call}=h;
const report={nativeWebView2:true,screenshots:false,mediaInterception:false,initialLibraryFailureInjected:true,phase:'guard'};
const nav=name=>page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name,exact:true});
const pagination=name=>page.getByRole('navigation',{name});
try{
 const q=await call('LoadQueue');assert.equal(q.entries[0].queueEntryId,'desktop05-a');assert.equal((await call('GetLocalFavoriteIDs')).length,1005);
 assert.equal((await call('GetSettings')).preferredQuality,'exhigh');
 report.phase='library-retry';
 // WebView2's native asset handler can bypass CDP routing. Inject at the
 // public fetch transport boundary, before the real application starts.
 await page.context().addInitScript(id=>{
  window.__auditInjected=0;const original=window.fetch;
  window.fetch=function(input,options){
   if(!window.__auditInjected&&String(input).endsWith('/wails/runtime')&&typeof options?.body==='string'){
    try{const b=JSON.parse(options.body);if(b.object===0&&b.args?.methodID===id){window.__auditInjected++;return Promise.reject(new Error('LOCAL_STORAGE_UNAVAILABLE'));}}catch{}
   }
   return original.call(this,input,options);
  };
 },h.id('GetLocalFavoriteIDs'));await h.reload();
 await nav('本地收藏').click();await page.getByRole('alert').filter({hasText:'本地音乐资料读取失败'}).first().waitFor();
 assert.equal(await page.evaluate(()=>window.__auditInjected),1);
 await page.getByRole('button',{name:'重试',exact:true}).click();
 await until(async()=>await page.locator('main .music-card').count()===200);
 await until(async()=>!(await page.locator('main .music-card button.favorite').first().isDisabled()));
 report.libraryRecoveredWithoutRestart=true;
 report.phase='favorites-page';
 for(let i=0;i<4;i++){await pagination('收藏翻页').getByRole('button',{name:'下一页'}).click();await page.getByText(`第 ${i+2} 页`,{exact:true}).waitFor();await until(async()=>await page.locator('main .music-card').count()===200);}
 const list=page.locator('main .music-list:visible');
 await list.evaluate(e=>{window.__auditEmpty=0;window.__auditObserver=new MutationObserver(()=>{if(!e.children.length)window.__auditEmpty++});window.__auditObserver.observe(e,{childList:true});});
 const beforeFav=(await call('GetLocalFavoriteIDs')).length;
 await page.locator('main .music-card button.favorite').first().click();
 await until(async()=>(await call('GetLocalFavoriteIDs')).length===beforeFav-1);
 await until(async()=>!(await pagination('收藏翻页').getByRole('button',{name:'下一页'}).isDisabled()));
 await page.getByText('第 5 页',{exact:true}).waitFor();assert.equal(await page.evaluate(()=>window.__auditEmpty),0);
 report.favoriteWritePreservedPageAndRows=true;
 report.phase='seek-persistence';
 const start=await call('LoadQueue');
 for(let i=0;i<50;i++)await page.getByRole('slider',{name:'播放进度',exact:true}).fill(String(5000+i*1000));
 await wait(600);const end=await call('LoadQueue');
 assert.equal(end.positionMs,54000);assert.ok(end.revision-start.revision>=1&&end.revision-start.revision<=3);
 report.seek={events:50,committedRevisions:end.revision-start.revision,finalMs:end.positionMs};
 await page.getByRole('slider',{name:'音量',exact:true}).fill('0.19');await until(async()=>(await call('GetSettings')).volume===.19);
 report.effectiveAnonymousQuality='exhigh';
 report.phase='history-page';await nav('播放历史').click();
 for(let i=0;i<4;i++){await pagination('历史翻页').getByRole('button',{name:'下一页'}).click();await page.getByText(`第 ${i+2} 页`,{exact:true}).waitFor();await until(async()=>await page.locator('main .music-card').count()===200);}
 const firstHistory=await page.locator('main .music-card-info').first().textContent();
 await page.locator('main .music-list:visible').evaluate(e=>{window.__auditObserver.disconnect();window.__auditEmpty=0;window.__auditObserver=new MutationObserver(()=>{if(!e.children.length)window.__auditEmpty++});window.__auditObserver.observe(e,{childList:true});});
 await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__auditAudioPlayers=new Set();HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__auditAudio=this;window.__auditAudioPlayers.add(this);return play.apply(this,args)};});
 await page.getByRole('button',{name:'播放',exact:true}).click();
 await page.waitForFunction(()=>window.__auditAudio&&!window.__auditAudio.paused&&window.__auditAudio.currentTime>54.5,null,{timeout:30000});
 await until(async()=>(await call('ListLocalHistory',1,0))[0].playedAt>1700000001004);
 await page.getByText('第 5 页',{exact:true}).waitFor();assert.equal(await page.locator('main .music-card-info').first().textContent(),firstHistory);assert.equal(await page.evaluate(()=>window.__auditEmpty),0);
 report.historyRecordingPreservedPageAndRows=true;
 const media=await page.evaluate(()=>({players:window.__auditAudioPlayers.size,duration:window.__auditAudio.duration,time:window.__auditAudio.currentTime}));
 assert.equal(media.players,1);assert.ok(Math.abs(media.duration-267.232)<2);report.realCachedPlayback=media;
 const s=await call('GetDesktopSnapshot');assert.ok(s.cacheHit);report.cachedProvider=s.provider;
 await page.getByRole('button',{name:'暂停',exact:true}).click();await page.getByRole('button',{name:'播放',exact:true}).waitFor();
 const cover=await call('ResolveCover',q.entries[0].track.coverRef);assert.match(cover,/^\/artwork\/[a-f0-9]{64}$/);
 report.artwork=await page.evaluate(async path=>{const start=performance.now();const codes=[];let cacheControl='';for(let i=0;i<20;i++){const r=await fetch(path);codes.push(r.status);cacheControl=r.headers.get('Cache-Control');await r.arrayBuffer();}return {requests:20,all200:codes.every(c=>c===200),wallMs:performance.now()-start,cacheControl};},cover);
 assert.ok(report.artwork.all200);report.phase='complete';report.pass=true;
}catch(e){report.pass=false;report.failure=e instanceof assert.AssertionError?'ASSERTION_FAILED':'NATIVE_CHECK_FAILED';report.failureKind=/strict mode violation/.test(e.message)?'AMBIGUOUS_TEST_LOCATOR':/Malformed value/.test(e.message)?'MALFORMED_TEST_INPUT':e.name;report.location=e.stack?.match(/native-audit051-acceptance\.mjs:\d+:\d+/)?.[0]??'HELPER';}
finally{await writeFile('output/playwright/native-audit051-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await h.browser.close();if(!report.pass)process.exitCode=1;}
