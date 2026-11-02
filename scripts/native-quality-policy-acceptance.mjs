// Real Wails policy/API and media smoke. No screenshots or network interception.
import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {chromium} from '../.tools/browser/node_modules/playwright/index.mjs';
const mod=(await readFile('go.mod','utf8')).match(/^module (.+)/m)[1].trim();
const source=await readFile(`frontend/bindings/${mod}/internal/appservice/appservice.ts`,'utf8');
const method=name=>Number(source.match(new RegExp(`function ${name}\\([^]*?ByID\\((\\d+)`))[1]);
let browser;
for(let i=0;i<40;i++){try{browser=await chromium.connectOverCDP('http://127.0.0.1:9225');break;}catch(e){if(i===39)throw e;await new Promise(r=>setTimeout(r,250));}}
const page=browser.contexts()[0].pages().find(p=>p.url().startsWith('http://wails.localhost/'));
const call=(name,...args)=>page.evaluate(async({id,args})=>(await import('/wails/runtime.js')).Call.ByID(id,...args),{id:method(name),args});
const report={native:true,screenshots:false,apiInterception:false,mediaInterception:false};
try {
  const policy=await call('GetPlaybackQualityPolicy');assert.equal(policy.premiumAllowed,false);assert.equal(policy.maxBitrate,320000);assert.deepEqual(policy.selectableQualities,['exhigh']);
  const settings=await call('GetSettings');assert.equal(settings.preferredQuality,'exhigh');assert.equal(settings.gdEnabled,true);assert.equal(settings.volume,0.37);report.legacyQualityNormalized=true;
  for(const q of ['standard','higher']) await assert.rejects(()=>call('SetSettings',{...settings,preferredQuality:q}),/QUALITY_FALLBACK_ONLY/);
  for(const q of ['lossless','hires','jyeffect','sky','jymaster']) await assert.rejects(()=>call('SetSettings',{...settings,preferredQuality:q,loggedIn:true,member:true,premiumAllowed:true}),/QUALITY_MEMBERSHIP_REQUIRED/);
  report.backendManualGuards=true;assert.equal((await call('GetSettings')).preferredQuality,'exhigh');
  const id=method('ResolveStream');
  report.requestedPremiumClamped=await page.evaluate(async id=>{
    const s=await(await import('/wails/runtime.js')).Call.ByID(id,'1850977722','hires');
    return {requestedQuality:s.requestedQuality,status:s.status,provider:s.provider,bitrate:s.actualBitrate,hasURL:Boolean(s.url),durationMs:s.durationMs};
  },id);
  assert.equal(report.requestedPremiumClamped.requestedQuality,'exhigh');assert.equal(report.requestedPremiumClamped.status,'full');assert.ok(report.requestedPremiumClamped.bitrate<=320000);
  await page.reload();await page.getByRole('button',{name:'播放',exact:true}).waitFor();
  await page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name:'设置',exact:true}).click();
  const selector=page.getByLabel('首选音质');assert.equal(await selector.inputValue(),'exhigh');assert.deepEqual(await selector.locator('option').evaluateAll(es=>es.map(e=>e.value)),['exhigh']);report.noManualLowOptions=true;
  await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__policyPlayers=new Set();HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__policyPlayers.add(this);window.__policyAudio=this;return play.apply(this,args);};});
  await page.getByRole('button',{name:'播放',exact:true}).click();await page.waitForFunction(()=>window.__policyAudio?.currentTime>1&&!window.__policyAudio.paused,undefined,{timeout:30000});
  report.nativeDuration=await page.evaluate(()=>window.__policyAudio.duration);assert.ok(Math.abs(report.nativeDuration-227.760)<2);assert.equal(await page.evaluate(()=>window.__policyPlayers.size),1);
  report.actualLabel=await page.locator('footer .stream-source').textContent();assert.match(report.actualLabel,/320 kbps/);report.realPlayback=true;report.singleAudio=true;
  await page.getByRole('button',{name:'暂停',exact:true}).click();report.pass=true;
}catch(error){report.pass=false;report.error=error.name;throw error;
}finally{await writeFile('output/playwright/quality-policy-native-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await browser.close();}
