// Actual native requests, observed method counts only. No response/RPC mocks or screenshots.
import assert from 'node:assert/strict';
import {readFile,writeFile,stat} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {nativeHarness,until,wait} from './native-harness.mjs';
const profile='output/native-audit-053';
const h=await nativeHarness(),{page,call}=h;
const report={nativeWebView2:true,rpcInterception:false,mediaInterception:false,screenshots:false,phase:'observe'};
const ids=Object.fromEntries(['GetStorageRecoveryStatus','GetAudioCacheStats','GetResourceCacheStats','GetRegionStatus'].map(n=>[n,h.id(n)]));
const counts=()=>page.evaluate(ids=>Object.fromEntries(Object.entries(ids).map(([name,id])=>[name,window.__audit053Counts[id]??0])),ids);
const nav=name=>page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name,exact:true});
const manifest=profile+'/cache/audio/manifest.json';
const hash=b=>createHash('sha256').update(b).digest('hex');
async function checkpoint(){await writeFile('output/playwright/native-audit053-report.json',JSON.stringify(report,null,2));}
try{
 await page.context().addInitScript(()=>{
  window.__audit053Counts={};const original=window.fetch;
  window.fetch=function(input,options){if(typeof options?.body==='string'){try{const b=JSON.parse(options.body);if(b.object===0&&Number.isSafeInteger(b.args?.methodID)){const id=b.args.methodID;window.__audit053Counts[id]=(window.__audit053Counts[id]??0)+1;}}catch{}}return original.call(this,input,options);};
 });
 await h.reload();
 await page.locator('.storage-recovery-notice').waitFor();
 assert.ok(!(await page.locator('.storage-recovery-notice').getAttribute('class')).split(' ').includes('error'));
  assert.notEqual(await page.getByRole('button',{name:'知道了，本次会话不再提示这次恢复',exact:true}).evaluate(e=>getComputedStyle(e).pointerEvents),'none');
 await page.getByRole('button',{name:'知道了，本次会话不再提示这次恢复',exact:true}).click();
 assert.equal(await page.locator('.storage-recovery-notice').count(),0);
 assert.equal((await call('GetStorageRecoveryStatus')).quarantinedFavorites,1);
 report.dismissRetainsArchives=true;
 report.phase='recency';
 await page.evaluate(()=>{const original=HTMLMediaElement.prototype.play;HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__audit053Audio=this;return original.apply(this,args);};});
 await page.getByRole('button',{name:'播放',exact:true}).click();
 await page.waitForFunction(()=>window.__audit053Audio&&!window.__audit053Audio.paused&&window.__audit053Audio.currentTime>.6,null,{timeout:30000});
 await page.getByRole('button',{name:'暂停',exact:true}).click();
 const before=await readFile(manifest);const beforeStat=await stat(manifest);
 await wait(6500);
 assert.equal(hash(await readFile(manifest)),hash(before));assert.equal((await stat(manifest)).mtimeMs,beforeStat.mtimeMs);
 const sidecar=await readFile(profile+'/cache/audio/recency.json');assert.ok(Object.keys(JSON.parse(sidecar)).length>=1);
 report.nativeRecency={manifestBytes:before.length,manifestSHA256:hash(before),manifestMtimeMs:beforeStat.mtimeMs,recencyBytes:sidecar.length,manifestContentAndMtimeUnchanged:true};
 report.phase='idle-polling';await page.evaluate(()=>window.__audit053Counts={});
 await checkpoint();await wait(8000);
 const idle=await counts();assert.equal(idle.GetStorageRecoveryStatus,0);assert.equal(idle.GetAudioCacheStats,0);assert.equal(idle.GetResourceCacheStats,0);assert.ok(idle.GetRegionStatus>=1);
 report.idleEightSeconds=idle;
 await nav('设置').click();await until(async()=>{const c=await counts();return c.GetAudioCacheStats>=1&&c.GetResourceCacheStats>=1;});await wait(200);
 await nav('发现音乐').click();const left=await counts();await wait(8000);const away=await counts();
 assert.equal(away.GetAudioCacheStats,left.GetAudioCacheStats);assert.equal(away.GetResourceCacheStats,left.GetResourceCacheStats);
 report.settingsUnmountStopsPolling=true;
 await nav('设置').click();await until(async()=>{const c=await counts();return c.GetAudioCacheStats>=left.GetAudioCacheStats+1;});
 const entered=await counts();await checkpoint();await wait(31000);
 const later=await until(async()=>{const c=await counts();return c.GetAudioCacheStats>entered.GetAudioCacheStats&&c.GetResourceCacheStats>entered.GetResourceCacheStats&&c.GetStorageRecoveryStatus>=1&&c;});
 assert.equal(await page.locator('.storage-recovery-notice').count(),0);report.dismissSurvivesSameStatusRefresh=true;report.realThirtySecondPolling=later;
 report.phase='new-recovery-status';
 execFileSync('python',['-c',"import sqlite3,sys;d=sqlite3.connect(sys.argv[1]);d.execute(\"INSERT INTO local_favorites(song_id,metadata,added_order,search_text)VALUES('989999999992','{audit053-second',100001,'old search')\");d.commit();d.close()",profile+'/library.db']);
 await call('SearchLocalFavorites','',200,0,'');
 await page.locator('.storage-recovery-notice').filter({hasText:'2 条'}).waitFor({timeout:35000});
 assert.equal((await call('GetStorageRecoveryStatus')).quarantinedFavorites,2);report.newStatusReappears=true;
 report.pass=true;report.phase='complete';
}catch(e){report.pass=false;report.failure=e instanceof assert.AssertionError?'ASSERTION_FAILED':'NATIVE_CHECK_FAILED';report.location=e.stack?.match(/native-audit053-acceptance\.mjs:\d+:\d+/)?.[0]??'HELPER';}
finally{await checkpoint();console.log(JSON.stringify(report,null,2));await h.browser.close();if(!report.pass)process.exitCode=1;}
