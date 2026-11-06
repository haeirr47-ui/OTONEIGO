import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import path from 'node:path';
import {until} from './native-harness.mjs';

// Real WebView2 keyboard path and real cached audio/settings/favorites. The only
// media instrumentation is the host acceptance harness's play counter.
export async function verifyShortcuts(h, report) {
  const {page,call}=h;
  const focusBody=async()=>{
    await page.bringToFront();
    await page.evaluate(async()=>{await window.__nativeTestRuntime.Window.Focus();document.body.tabIndex=-1;document.body.focus();});
    await page.waitForFunction(()=>document.hasFocus()&&document.activeElement===document.body,undefined,{timeout:3000});
  };
  const shortcutButton=page.getByRole('button',{name:'快捷键',exact:true});
  await shortcutButton.waitFor({state:'visible'});
  await focusBody();
  assert.equal(await page.evaluate(()=>document.hasFocus()),true,'native main window is focused before shortcut dispatch');
  await page.evaluate(()=>{window.__shortcutKeyObservation=[];window.__shortcutKeyListener=e=>window.__shortcutKeyObservation.push({key:e.key,code:e.code,shift:e.shiftKey,ctrl:e.ctrlKey,prevented:e.defaultPrevented,target:e.target?.tagName,focus:document.hasFocus()});document.addEventListener('keydown',window.__shortcutKeyListener,true);});
  await page.keyboard.press('?');
  await page.evaluate(()=>document.removeEventListener('keydown',window.__shortcutKeyListener,true));
  const help=page.getByRole('dialog',{name:'快捷键'});
  try { await help.waitFor({state:'visible',timeout:2000}); }
  catch { throw new Error(`native '?' shortcut did not open help; key=${JSON.stringify(await page.evaluate(()=>window.__shortcutKeyObservation))}`); }
  for(const label of ['播放 / 暂停（加载中可取消）','上一首','下一首','音量增加 5%','音量减少 5%','后退 5 秒','前进 5 秒','收藏 / 取消收藏当前歌曲','打开播放队列','打开歌词','打开快捷键帮助'])
    assert.ok(await help.getByText(label,{exact:true}).count(),`help lists ${label}`);
  assert.equal(await page.locator('[role="dialog"]').count(),1);
  await page.keyboard.press('q');
  assert.equal(await page.locator('[role="dialog"]').count(),1,'help owns the shortcut scope');
  assert.equal(await page.getByRole('dialog',{name:'播放队列'}).count(),0);
  await page.evaluate(()=>window.__nativeTestRuntime.Window.SetSize(900,640));
  await page.waitForFunction(()=>innerWidth<=900&&innerHeight<=640,undefined,{timeout:5000});
  const bounds=await help.evaluate(el=>{const r=el.getBoundingClientRect(),c=el.querySelector('button[aria-label="关闭快捷键"]')?.getBoundingClientRect();return {left:r.left,top:r.top,right:r.right,bottom:r.bottom,scroll:el.scrollHeight>el.clientHeight,client:el.clientWidth,scrollWidth:el.scrollWidth,close:c?{bottom:c.bottom}:null};});
  const viewport=await page.evaluate(()=>({width:innerWidth,height:innerHeight}));
  assert.ok(bounds.left>=0&&bounds.top>=0&&bounds.right<=viewport.width&&bounds.bottom<=viewport.height,'help dialog stays within minimum window bounds');
  assert.ok(bounds.scrollWidth<=bounds.client+1,'help content has no horizontal overflow');
  assert.ok(bounds.close&&bounds.close.bottom<=bounds.bottom,'help close control is inside the dialog');
  if(bounds.scroll) await help.evaluate(el=>{el.scrollTop=el.scrollHeight;});
  assert.ok(await help.getByRole('button',{name:'关闭快捷键',exact:true}).isVisible(),'close control remains reachable after scrolling');
  const image=path.resolve('output/playwright/native-shortcuts-help.png');
  await mkdir(path.dirname(image),{recursive:true});await page.screenshot({path:image});
  await page.keyboard.press('Tab');
  assert.equal(await help.evaluate(el=>el.contains(document.activeElement)),true,'Tab remains within help');
  await page.keyboard.press('Escape');await help.waitFor({state:'detached'});
  assert.equal(await page.evaluate(()=>document.activeElement===document.body),true,'keyboard help restores body focus');
  await page.evaluate(()=>window.__nativeTestRuntime.Window.SetSize(1200,800));
  await shortcutButton.click();await help.waitFor({state:'visible'});
  await page.keyboard.press('Escape');await help.waitFor({state:'detached'});
  assert.equal(await shortcutButton.evaluate(el=>el===document.activeElement),true,'button help restores its opener');
  report.shortcutHelpRegistryAndFocus=true;report.shortcutHelpScreenshot=image;

  const queueOpener=page.locator('footer').getByRole('button',{name:'播放队列',exact:true});
  await page.evaluate(()=>document.body.focus());
  await page.keyboard.press('q');
  const queue=page.getByRole('dialog',{name:'播放队列'});await queue.waitFor({state:'visible'});
  assert.equal(await page.locator('[role="dialog"]').count(),1);
  await page.keyboard.press('Escape');await queue.waitFor({state:'detached'});
  await queueOpener.waitFor();
  const lyricsOpener=page.locator('footer').getByRole('button',{name:'展开歌词',exact:true}).last();
  await page.evaluate(()=>document.body.focus());
  await page.keyboard.press('l');
  const lyrics=page.getByRole('dialog',{name:'歌词视图'});await lyrics.waitFor({state:'visible'});
  await page.keyboard.press('Escape');await lyrics.waitFor({state:'detached'});
  report.shortcutQueueAndLyrics=true;

  const track=(await call('LoadQueue')).entries[(await call('LoadQueue')).currentIndex].track;
  await page.evaluate(()=>document.body.focus());
  const wasFavorite=(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId);
  await page.keyboard.press('f');
  await until(async()=>(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId)!==wasFavorite);
  const fav=page.locator('footer').getByRole('button',{name:`${wasFavorite?'收藏':'取消收藏'}：${track.name}`,exact:true});
  assert.equal(await fav.getAttribute('aria-pressed'),String(!wasFavorite));
  await page.keyboard.press('f');
  await until(async()=>(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId)===wasFavorite);
  report.shortcutFavoriteSync=true;

  // Editing suppresses application shortcuts. Use a temporary real focused input.
  await page.evaluate(()=>{const input=document.createElement('input');input.id='native-shortcut-fixture';document.body.append(input);input.focus();});
  await page.keyboard.press('f');
  assert.equal((await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId),wasFavorite,'input focus suppresses favorite');
  await page.keyboard.press(' ');
  assert.equal(await page.evaluate(()=>window.__selectorAudio.paused),true,'input focus suppresses Space playback');
  await page.evaluate(()=>document.querySelector('#native-shortcut-fixture')?.remove());
  await page.evaluate(()=>document.body.focus());
  report.shortcutEditorGuards=true;

  const settingsBefore=await call('GetSettings');
  const volume=page.getByRole('slider',{name:'音量',exact:true});
  const setVolumeThroughUI=async value=>{await volume.fill(String(value));await until(async()=>Math.abs((await call('GetSettings')).volume-value)<0.001);};
  await setVolumeThroughUI(0.4);
  const baselineVolume=(await call('GetSettings')).volume;
  await page.getByRole('button',{name:'播放',exact:true}).focus();
  await page.keyboard.press(' ');
  await page.waitForFunction(()=>window.__selectorAudio&&!window.__selectorAudio.paused,undefined,{timeout:5000});
  const audioBefore=await page.evaluate(()=>window.__selectorAudio.currentTime);
  await page.evaluate(()=>document.body.focus());
  await page.keyboard.press('ArrowRight');
  await page.waitForFunction(t=>window.__selectorAudio.currentTime>=t+4.9, audioBefore, {timeout:2000});
  await page.keyboard.press('Control+ArrowUp');
  await until(async()=>(await call('GetSettings')).volume>=Math.min(1,baselineVolume+0.049));
  await page.keyboard.press('Control+ArrowDown');
  await until(async()=>(await call('GetSettings')).volume<=baselineVolume+0.001);
  await setVolumeThroughUI(settingsBefore.volume);
  await until(async()=>Math.abs((await call('GetSettings')).volume-settingsBefore.volume)<0.001);
  await page.evaluate(()=>document.body.focus());
  await page.keyboard.press(' ');
  await page.waitForFunction(()=>window.__selectorAudio.paused,undefined,{timeout:5000});
  report.shortcutNativeAudioSeekAndVolume=true;

  await focusBody();await page.keyboard.press(' ');
  await page.waitForFunction(()=>!window.__selectorAudio.paused,undefined,{timeout:5000});
  await focusBody();await page.keyboard.press(' ');
  await page.waitForFunction(()=>window.__selectorAudio.paused,undefined,{timeout:5000});
  report.shortcutSpaceAndNativeButtonSpace=true;

  await page.evaluate(()=>{const editor=document.createElement('div');editor.id='native-shortcut-editor';editor.contentEditable='true';document.body.append(editor);editor.focus();});
  await page.keyboard.press('q');await page.keyboard.press('f');await page.keyboard.press(' ');
  assert.equal(await page.locator('[role="dialog"]').count(),0);
  assert.equal(await page.evaluate(()=>window.__selectorAudio.paused),true);
  assert.equal((await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId),wasFavorite);
  await page.evaluate(()=>document.querySelector('#native-shortcut-editor')?.remove());
  report.shortcutContenteditableGuard=true;
  await focusBody();

  // Synthetic edge cases still traverse the installed document listener.
  const beforeSynthetic=(await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId);
  await page.evaluate(()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'f',bubbles:true,repeat:true})));
  await page.evaluate(()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'Process',keyCode:229,bubbles:true})));
  await page.evaluate(()=>{const e=new KeyboardEvent('keydown',{key:'f',bubbles:true});Object.defineProperty(e,'isComposing',{value:true});document.dispatchEvent(e);});
  assert.equal((await call('GetLocalFavorites')).some(x=>x.neteaseSongId===track.neteaseSongId),beforeSynthetic,'repeat, IME and composition events do not toggle favorite');
  report.shortcutRepeatAndImeGuards=true;

  await focusBody();await page.keyboard.press('Control+ArrowLeft');
  await until(async()=>(await call('LoadQueue')).currentIndex===0);
  await page.waitForFunction(()=>!window.__selectorAudio.paused&&window.__selectorAudio.currentTime>0.15,undefined,{timeout:10000});
  await focusBody();await page.keyboard.press('Control+ArrowRight');
  await until(async()=>(await call('LoadQueue')).currentIndex===1);
  await page.waitForFunction(()=>!window.__selectorAudio.paused&&window.__selectorAudio.currentTime>0.15,undefined,{timeout:10000});
  await focusBody();await page.keyboard.press(' ');
  await page.waitForFunction(()=>window.__selectorAudio.paused);
  report.shortcutPreviousNext=true;

  // Physical/global OS hotkeys are outside this browser acceptance check.
  assert.ok(await page.evaluate(()=>window.__selectorPlayers.size===1),'native app retains one real audio element');
}
