import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {chromium} from '../.tools/browser/node_modules/playwright/index.mjs';

export const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
export async function until(test,timeout=10000){
  const end=Date.now()+timeout;
  while(Date.now()<end){const result=await test();if(result)return result;await wait(100);}
  throw Error('NATIVE_CONDITION_TIMEOUT');
}
export async function nativeHarness(){
  const mod=(await readFile('go.mod','utf8')).match(/^module (.+)/m)[1].trim();
  const binding=await readFile(`frontend/bindings/${mod}/internal/appservice/appservice.ts`,'utf8');
  const id=name=>Number(binding.match(new RegExp(`function ${name}\\([^]*?ByID\\((\\d+)`))[1]);
  let browser;
  for(let n=0;n<40;n++){try{browser=await chromium.connectOverCDP('http://127.0.0.1:9225');break;}catch(e){if(n===39)throw e;await wait(250);}}
  // CDP can start listening before WebView2 has created/navigated its page.
  let page;
  for(let n=0;n<80;n++){
    page=browser.contexts().flatMap(context=>context.pages()).find(p=>{
      if(!p.url().startsWith('http://wails.localhost/'))return false;
      return !new URL(p.url()).searchParams.has('view');
    });
    if(page)break;
    await wait(125);
  }
  assert.ok(page,'native application page created after CDP startup');
  async function initialise(){
    await page.locator('footer[aria-label="播放器"]').waitFor();
    await page.evaluate(async()=>{
      // The SDK module installs its own event map. Restore the app dispatcher
      // immediately so tray and normal-close handlers remain the actual ones.
      const dispatch=window._wails.dispatchWailsEvent;
      window.__nativeTestRuntime=await import('/wails/runtime.js');
      window._wails.dispatchWailsEvent=dispatch;
    });
  }
  await initialise();
  const call=(name,...args)=>page.evaluate(async({id,args})=>window.__nativeTestRuntime.Call.ByID(id,...args),{id:id(name),args});
  const event=(name,data)=>page.evaluate(async({name,data})=>window.__nativeTestRuntime.Events.Emit(name,data),{name,data});
  const reload=async()=>{await page.reload();await initialise();};
  // A previous tray test can leave the native surface hidden. Restore the
  // isolated host before issuing real pointer interactions.
  await call('ShowMainWindow');
  return{browser,page,id,call,event,reload};
}
