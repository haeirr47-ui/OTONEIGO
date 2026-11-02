// Native lifecycle/geometry checks; test profile only, no playback interception.
import assert from 'node:assert/strict';
import {writeFile} from 'node:fs/promises';
import {nativeHarness,until,wait} from './native-harness.mjs';
const mode=process.argv[2]??'restore';
const h=await nativeHarness(),{page,call}=h;
const report={mode,nativeWebView2:true,screenshots:false};
const pages=()=>h.browser.contexts().flatMap(c=>c.pages());
const find=kind=>until(()=>pages().find(p=>p.url().includes(`?view=${kind}`)));
const setup=async p=>p.evaluate(async()=>{const original=window._wails.dispatchWailsEvent;window.__nativeTestRuntime=await import('/wails/runtime.js');window._wails.dispatchWailsEvent=original;});
try{
 await until(async()=>(await call('GetDesktopSnapshot')).ready);
 assert.equal((await call('GetDesktopSnapshot')).playback,'paused');
 if(mode==='restore')assert.equal(pages().filter(p=>p.url().includes('?view=')).length,0,'auxiliary windows are lazy after restart');
 const settings=await call('GetSettings');
 await call('ShowDesktopView','mini');await call('ShowDesktopView','lyrics');
 const mini=await find('mini'),lyrics=await find('lyrics');
 await mini.getByRole('main',{name:'迷你播放器',exact:true}).waitFor();
 await lyrics.getByRole('main',{name:'桌面歌词',exact:true}).waitFor();
 await setup(mini);await setup(lyrics);
 const position=p=>p.evaluate(()=>window.__nativeTestRuntime.Window.Position());
 const mp=await position(mini),lp=await position(lyrics);
 assert.deepEqual(mp,{x:settings.desktop.miniPlacement.x,y:settings.desktop.miniPlacement.y});
 assert.deepEqual(lp,{x:settings.desktop.lyricsPlacement.x,y:settings.desktop.lyricsPlacement.y});
 report.restoredPositions={mini:mp,lyrics:lp};if(mode==='restore')report.defaultOnlyMainWindow=true;
 if(mode==='restore'){
   const state=await call('GetDesktopNativeState');assert.ok(state.taskbarAvailable);
   await call('SetSettings',{...settings,desktop:{...settings.desktop,taskbarProgress:false}});
   await until(async()=>(await call('GetDesktopNativeState')).taskbarState==='none');
   await call('SetSettings',settings);
   await until(async()=>(await call('GetDesktopNativeState')).taskbarState==='paused');
   report.taskbarCanDisable=true;
   await page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name:'设置',exact:true}).click();
   await page.getByRole('slider',{name:'歌词字号',exact:true}).press('ArrowRight');
   await until(async()=>(await call('GetSettings')).desktop.lyricsFontSize===29);
   await until(async()=>(await lyrics.locator('main').evaluate(e=>getComputedStyle(e).getPropertyValue('--lyrics-size'))).trim()==='29px');
   await page.getByRole('slider',{name:'歌词不透明度',exact:true}).press('ArrowLeft');
   await until(async()=>Math.abs((await call('GetSettings')).desktop.lyricsOpacity-.85)<.001);
   await page.getByRole('slider',{name:'歌词字号',exact:true}).press('ArrowLeft');
   await page.getByRole('slider',{name:'歌词不透明度',exact:true}).press('ArrowRight');
   await until(async()=>{const s=await call('GetSettings');return s.desktop.lyricsFontSize===28&&Math.abs(s.desktop.lyricsOpacity-.9)<.001;});
   report.nativeLyricsStyleSettings=true;
   await call('ResetDesktopViewPositions');await wait(450);
   const resetBounds=[];
   for(const p of [mini,lyrics]){
     const b=await p.evaluate(async()=>({position:await window.__nativeTestRuntime.Window.Position(),size:await window.__nativeTestRuntime.Window.Size(),screen:await window.__nativeTestRuntime.Window.GetScreen()}));
     const a=b.screen.WorkArea;
     assert.ok(b.position.x>=a.X&&b.position.y>=a.Y&&b.position.x+b.size.width<=a.X+a.Width+1&&b.position.y+b.size.height<=a.Y+a.Height+1);
     resetBounds.push({position:b.position,size:b.size,scaleFactor:b.screen.ScaleFactor});
   }
   report.resetWithinActualWorkArea=resetBounds;
   await mini.evaluate(p=>window.__nativeTestRuntime.Window.SetPosition(p.x,p.y),mp);
   await lyrics.evaluate(p=>window.__nativeTestRuntime.Window.SetPosition(p.x,p.y),lp);await wait(450);
   // Native minimum window sizing: check viewport/footer without screenshots.
   await page.evaluate(()=>window.__nativeTestRuntime.Window.SetSize(900,640));await wait(300);
   report.minimumLayout=await page.evaluate(()=>{
     const f=document.querySelector('footer[aria-label="播放器"]'),r=f.getBoundingClientRect();
     return {width:innerWidth,height:innerHeight,horizontalOverflow:document.documentElement.scrollWidth>innerWidth,footerVisible:r.bottom<=innerHeight+1&&r.top>=0};
   });
   assert.equal(report.minimumLayout.horizontalOverflow,false);assert.equal(report.minimumLayout.footerVisible,true);
   await page.evaluate(()=>window.__nativeTestRuntime.Window.SetSize(1200,800));
 }else{
   if(mode==='close-hidden'){
     await call('HideDesktopView','mini');await call('HideDesktopView','lyrics');
     const n=await call('GetDesktopNativeState');assert.equal(n.miniVisible,false);assert.equal(n.lyricsVisible,false);
     report.auxiliaryHiddenBeforeClose=true;
   }else if(mode==='close-visible'){
     await mini.evaluate(()=>window.__nativeTestRuntime.Window.SetPosition(95,105));
     await lyrics.evaluate(()=>window.__nativeTestRuntime.Window.SetPosition(115,315));
     report.immediateClosePlacements={mini:{x:95,y:105},lyrics:{x:115,y:315}};
   }else throw Error('INVALID_MODE');
   const terminal=[];
   for(const p of [mini,lyrics]){
     await p.exposeFunction('__recordDesktopTerminal',s=>terminal.push(s));
     await p.evaluate(()=>{
       const dispatch=window._wails.dispatchWailsEvent;
       window._wails.dispatchWailsEvent=e=>{
         if(e.name==='otoneigo:desktop-snapshot'&&e.data.closing)void window.__recordDesktopTerminal({closing:e.data.closing,ready:e.data.ready,epoch:e.data.epoch,sequence:e.data.sequence});
         return dispatch(e);
       };
     });
   }
   try{await page.evaluate(()=>window.__nativeTestRuntime.Window.Close());}catch(e){if(!/closed|Target|destroyed/.test(e.message))throw e;}
   await wait(300);
   assert.ok(terminal.length>=2,'both auxiliary windows receive terminal projection');
   assert.ok(terminal.every(s=>s.closing&&!s.ready));report.terminalSnapshots=terminal.length;
   report.nativeCloseRequested=true;
 }
 report.pass=true;
}catch(e){report.pass=false;report.failure=e instanceof assert.AssertionError?'ASSERTION_FAILED':'NATIVE_CHECK_FAILED';report.location=e.stack?.match(/native-desktop05-lifecycle\.mjs:\d+:\d+/)?.[0]??'HELPER';}
finally{await writeFile(`output/playwright/native-desktop05-${mode}-report.json`,JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await h.browser.close();if(!report.pass)process.exitCode=1;}
