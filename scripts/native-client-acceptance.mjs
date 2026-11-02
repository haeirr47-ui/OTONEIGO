// Explicit native WebView2 acceptance. No network or Wails method interception.
// Requires the isolated p0debug build on loopback port 9225.
import assert from 'node:assert/strict';
import {readFile, mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {chromium} from '../.tools/browser/node_modules/playwright/index.mjs';
const root=process.cwd(), output=path.join(root,'output/playwright');
await mkdir(output,{recursive:true});
const mod=(await readFile('go.mod','utf8')).match(/^module (.+)/m)[1].trim();
const binding=await readFile(`frontend/bindings/${mod}/internal/appservice/appservice.ts`,'utf8');
const id=name=>Number(binding.match(new RegExp(`function ${name}\\([^]*?ByID\\((\\d+)`))[1]);
const methods=Object.fromEntries(['LoadQueue','SaveQueue','SearchNetease','GetSettings','SetSettings','GetRegionStatus','GetCapabilities','ListLocalHistory','GetLocalFavorites','ResolveStream'].map(n=>[n,id(n)]));
const browser=await chromium.connectOverCDP('http://127.0.0.1:9225');
const page=browser.contexts()[0].pages().find(p=>p.url().startsWith('http://wails.localhost/'));
assert.ok(page,'native page');
const call=(name,...args)=>page.evaluate(async({id,args})=>(await import('/wails/runtime.js')).Call.ByID(id,...args),{id:methods[name],args});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,timeout=10000){const end=Date.now()+timeout;while(Date.now()<end){const value=await fn();if(value)return value;await sleep(150);}throw new Error('ACCEPTANCE_CONDITION_TIMEOUT');}
const report={kind:'actual-production-service-native-webview2',bindingInterception:false,mediaInterception:false,muted:true,songId:'33894312',resolved:[]};
page.on('response',async response=>{
  try{if(!response.url().endsWith('/wails/runtime'))return;const request=response.request().postDataJSON();if(request?.args?.methodID!==methods.ResolveStream)return;const v=await response.json();report.resolved.push({songId:v.songId,provider:v.provider,status:v.status,strategy:v.accessStrategy,errorCode:v.errorCode,quality:v.actualQuality,format:v.actualFormat,bitrate:v.actualBitrate,hasURL:Boolean(v.url),durationMs:v.durationMs});}catch{}
});
let heartbeat;
try{
  const capabilities=await call('GetCapabilities');assert.equal(capabilities.gd,true);assert.equal(capabilities.account,false);
  const search=await call('SearchNetease','情非得已 童声版',0);
  const track=search.tracks.find(t=>t.neteaseSongId===report.songId);assert.ok(track,'real search target');report.metadataDurationMs=track.durationMs;
  const one=crypto.randomUUID(),two=crypto.randomUUID();
  const old=await call('LoadQueue');
  await call('SetSettings',{preferredQuality:'exhigh',regionMode:'off',gdEnabled:false,volume:0.3});
  await call('SaveQueue',{entries:[{queueEntryId:one,track},{queueEntryId:two,track}],currentEntryId:one,currentIndex:0,playMode:'repeat-all',positionMs:0,revision:(old?.revision??0)+1});
  await page.reload();await page.getByRole('button',{name:'播放',exact:true}).waitFor();
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__nativePlayers=new Set();window.__nativePlays=0;window.__nativeEnded=[];HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__nativePlayers.add(this);window.__nativeAudio=this;window.__nativePlays++;if(!this.__observed){this.__observed=true;this.addEventListener('ended',()=>window.__nativeEnded.push({time:this.currentTime,duration:this.duration}),{capture:true});}return play.apply(this,args);};});
  const nav=name=>page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name,exact:true});
  await page.getByLabel('搜索歌曲或歌手').fill('情非得已 童声版');
  await page.locator('form').getByRole('button',{name:'搜索',exact:true}).click();
  await page.locator('main .music-card-info').first().waitFor();report.realSearchUI=true;
  await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.getByRole('alert').first().waitFor({timeout:18000});
  assert.equal((await call('ListLocalHistory',200,0)??[]).length,0);report.normalModeFailedWithoutHistory=true;
  await nav('设置').click();
  await page.getByLabel('海外播放兼容').selectOption('auto');
  await until(async()=>(await call('GetSettings')).regionMode==='auto');
  await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.waitForFunction(()=>window.__nativeAudio?.currentTime>1&&!window.__nativeAudio.paused,undefined,{timeout:22000});
  report.compatNativeDurationSeconds=await page.evaluate(()=>window.__nativeAudio.duration);
  assert.ok(Math.abs(report.compatNativeDurationSeconds-track.durationMs/1000)<2);
  report.region=await call('GetRegionStatus');
  await until(async()=>(await call('ListLocalHistory',200,0)??[]).length===1);
  await page.locator('footer .favorite').click();
  await until(async()=>(await call('GetLocalFavorites')??[]).some(t=>t.neteaseSongId===track.neteaseSongId));
  const source=await page.evaluate(()=>window.__nativeAudio.currentSrc);
  await page.getByLabel('首选音质').selectOption('exhigh');
  await page.getByLabel('海外播放兼容').selectOption('off');
  await until(async()=>{const s=await call('GetSettings');return s.regionMode==='off'&&s.preferredQuality==='exhigh';});
  assert.equal(await page.evaluate(()=>window.__nativeAudio.currentSrc),source);assert.equal(await page.evaluate(()=>window.__nativeAudio.paused),false);report.policyChangePreservedActiveAudio=true;
  await page.getByRole('button',{name:'暂停',exact:true}).click();
  const slider=page.getByRole('slider',{name:'播放进度'});await slider.fill('81000');await slider.dispatchEvent('change');
  await until(async()=>(await call('LoadQueue')).positionMs===81000);report.persistedSeekMs=81000;
  await page.getByLabel('首选音质').selectOption('exhigh');
  await page.getByLabel('启用 GD 网易云备用音源',{exact:true}).check();
  await until(async()=>{const s=await call('GetSettings');return s.gdEnabled&&s.preferredQuality==='exhigh';});
  await slider.fill('0');await slider.dispatchEvent('change');
  await page.getByRole('button',{name:'播放',exact:true}).click();
  await page.waitForFunction(()=>window.__nativeAudio?.currentTime>1&&!window.__nativeAudio.paused,undefined,{timeout:22000});
  await until(async()=>(await page.locator('footer .stream-source').textContent()).startsWith('GD 网易云备用音源 · '));
  report.gdNativeDurationSeconds=await page.evaluate(()=>window.__nativeAudio.duration);
  assert.ok(Math.abs(report.gdNativeDurationSeconds-track.durationMs/1000)<2);
  await page.screenshot({path:path.join(output,'native-production-gd-playing.png')});
  console.log(JSON.stringify({stage:'actual-gd-playing',duration:report.gdNativeDurationSeconds,region:report.region.country}));
  const start=Date.now();
  heartbeat=setInterval(async()=>{try{console.log(JSON.stringify({stage:'actual-natural-playback',...await page.evaluate(()=>({time:window.__nativeAudio.currentTime,ended:window.__nativeEnded.length}))}));}catch{}},40000);
  await page.waitForFunction(()=>window.__nativeEnded.length>=1&&window.__nativeAudio.currentTime>0.5&&!window.__nativeAudio.paused,undefined,{timeout:310000});
  clearInterval(heartbeat);heartbeat=undefined;
  report.gdNaturalEnd=await page.evaluate(()=>window.__nativeEnded[0]);report.naturalEndWallSeconds=(Date.now()-start)/1000;
  assert.ok(report.gdNaturalEnd.time>260);assert.equal(await page.evaluate(()=>window.__nativePlayers.size),1);
  report.singleAudioExecutor=true;report.automaticNextEntry=(await call('LoadQueue')).currentEntryId===two;assert.ok(report.automaticNextEntry);
  await until(async()=>(await call('ListLocalHistory',200,0)??[]).length>=3);
  await page.getByRole('button',{name:'暂停',exact:true}).click();
  await nav('本地收藏').click();assert.ok(await page.getByRole('region',{name:'本地收藏',exact:true}).getByRole('button',{name:`播放 ${track.name} - ${track.artists.map(a=>a.name).join(' / ')}`,exact:true}).count());
  await nav('播放历史').click();assert.ok(await page.getByRole('region',{name:'播放历史',exact:true}).getByRole('button',{name:`播放 ${track.name} - ${track.artists.map(a=>a.name).join(' / ')}`,exact:true}).count());
  report.libraryUI=true;report.historyCount=(await call('ListLocalHistory',200,0)).length;
  const saved=await call('LoadQueue');assert.equal(saved.entries.length,2);assert.ok(!JSON.stringify(saved).includes('https://m'));report.metadataOnlyQueue=true;
  // Native resize, rather than Chromium viewport emulation.
  await page.evaluate(async()=>{const w=(await import('/wails/runtime.js')).Window;await w.SetSize(900,640);});
  await sleep(400);await page.screenshot({path:path.join(output,'native-production-min-window.png')});
  report.layout=await page.evaluate(()=>{const p=document.querySelector('footer').getBoundingClientRect();return{width:innerWidth,height:innerHeight,playerBottom:p.bottom,scrollWidth:document.documentElement.scrollWidth,scrollHeight:document.documentElement.scrollHeight};});
  assert.ok(report.layout.playerBottom<=report.layout.height+1,'native player stays visible');
  assert.ok(report.layout.scrollWidth<=report.layout.width,'no horizontal overflow');
  await writeFile(path.join(output,'native-production-report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
}catch(error){
  report.failed=true;report.failure=error.message.includes('ACCEPTANCE')?error.message:error.name;
  report.safeState=await page.evaluate(()=>({time:window.__nativeAudio?.currentTime,duration:window.__nativeAudio?.duration,paused:window.__nativeAudio?.paused,mediaError:window.__nativeAudio?.error?.code,alerts:[...document.querySelectorAll('[role=alert]')].map(e=>e.textContent)})).catch(()=>({}));
  await writeFile(path.join(output,'native-production-report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));throw new Error('NATIVE_ACCEPTANCE_FAILED');
}finally{if(heartbeat)clearInterval(heartbeat);await browser.close();}
