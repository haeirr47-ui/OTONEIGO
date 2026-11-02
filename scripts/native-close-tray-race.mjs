import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {nativeHarness,until,wait} from './native-harness.mjs';
const targetPID=Number(process.argv.find(a=>a.startsWith('--pid='))?.slice(6));
assert.ok(Number.isSafeInteger(targetPID)&&targetPID>0,'Explicit isolated process PID required');
const h=await nativeHarness(),{page,call}=h;
assert.ok((await call('LoadQueue')).entries.some(e=>e.track.name.startsWith('OTONEIGO 缓存原生验收')));
let s=await call('GetSettings');await call('SetSettings',{...s,closeToTray:false});
await h.reload();
const pause=page.getByRole('button',{name:'暂停',exact:true});if(await pause.count())await pause.click();
await page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name:'设置',exact:true}).click();
let prepareCount=0,ackCount=0,delayed=false,unblock;
const blocked=new Promise(resolve=>{unblock=resolve;});
await page.exposeFunction('__nativeCloseObserved',()=>prepareCount++);
await page.evaluate(()=>{const dispatch=window._wails.dispatchWailsEvent;window._wails.dispatchWailsEvent=e=>{if(e.name==='otoneigo:prepare-close')void window.__nativeCloseObserved();return dispatch(e);};});
page.on('request',r=>{if(r.url().endsWith('/wails/runtime')&&r.postDataJSON()?.args?.name==='otoneigo:close-ready')ackCount++;});
await page.route('**/wails/runtime',async route=>{if(route.request().postDataJSON()?.args?.methodID===h.id('SetSettings')&&!delayed){delayed=true;await blocked;}await route.continue().catch(()=>{});});
await page.getByLabel('关闭窗口时隐藏到托盘').check();
await until(()=>delayed);
// Reuse the bounded, PID-scoped native WM_CLOSE helper from existing acceptance.
const source=await readFile('scripts/native-state-acceptance.mjs','utf8');
const template=source.match(/const closeCommand = `([^]*?)`;/)[1];
assert.ok(template.includes('${targetPID}'));
const command=template.replaceAll('${targetPID}',String(targetPID));
const closed=new Promise(resolve=>page.once('close',resolve)),start=Date.now();
await promisify(execFile)('powershell.exe',['-NoProfile','-Command',command],{windowsHide:true});
// PowerShell startup is variable. Release only after the actual WM_CLOSE has
// entered the old-policy close gate, then keep the settings request slow.
await until(()=>prepareCount===1);await wait(1000);unblock();
let timeout;
try{await Promise.race([closed,new Promise((_,reject)=>{timeout=setTimeout(()=>reject(Error('NATIVE_CLOSE_TIMEOUT')),6000);})]);}finally{clearTimeout(timeout);}
assert.equal(prepareCount,1);assert.equal(ackCount,1);assert.equal(delayed,true);
const report={native:true,pidScopedWM_CLOSE:true,delayedSettingsMs:1000,prepareCount,ackCount,elapsedMs:Date.now()-start,exitedInsteadOfHiding:true,pass:true};
await writeFile('output/playwright/native-close-tray-race-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report));await h.browser.close();
