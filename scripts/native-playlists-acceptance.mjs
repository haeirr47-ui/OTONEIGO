// Native Wails/SQLite acceptance. No API/media fixtures or screenshots.
import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {chromium} from '../.tools/browser/node_modules/playwright/index.mjs';
const mod=(await readFile('go.mod','utf8')).match(/^module (.+)/m)[1].trim();
const binding=await readFile(`frontend/bindings/${mod}/internal/appservice/appservice.ts`,'utf8');
const method=n=>Number(binding.match(new RegExp(`function ${n}\\([^]*?ByID\\((\\d+)`))[1]);
const browser=await chromium.connectOverCDP('http://127.0.0.1:9225');
const page=browser.contexts()[0].pages().find(p=>p.url().startsWith('http://wails.localhost/'));
const call=(name,...args)=>page.evaluate(async({id,args})=>(await import('/wails/runtime.js')).Call.ByID(id,...args),{id:method(name),args});
const pause=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,timeout=10000){const end=Date.now()+timeout;while(Date.now()<end){const value=await fn();if(value)return value;await pause(100);}throw Error('PLAYLIST_ACCEPTANCE_TIMEOUT');}
const report={native:true,realSQLite:true,apiInterception:false,mediaInterception:false,screenshots:false};
const nav=name=>page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name,exact:true});
const playlists=()=>page.getByRole('region',{name:'本地歌单',exact:true});
const detail=()=>page.getByRole('region',{name:'本地歌单详情',exact:true});
const list=()=>call('ListLocalPlaylists');
const find=async name=>(await list()).find(p=>p.name===name);
async function openList(){await nav('本地收藏').click();await page.getByRole('button',{name:'本地歌单',exact:true}).click();}
async function openPlaylist(name){await playlists().getByRole('button').filter({has:page.getByRole('heading',{name,exact:true})}).click();await detail().waitFor();await detail().getByRole('heading',{name,exact:true}).waitFor();}
try {
  if(process.argv.includes('--verify')) {
    await page.getByRole('button',{name:'播放',exact:true}).waitFor();
    const saved=(await list()).find(p=>p.name==='关窗保存歌单');assert.ok(saved);
    const p=await call('GetLocalPlaylist',saved.playlistId);assert.equal(p.tracks[0].neteaseSongId,'1850977722');
    await openList();await openPlaylist('关窗保存歌单');assert.equal(await detail().locator('.playlist-track').count(),1);
    assert.equal((await call('GetSettings')).gdEnabled,true);assert.equal((await call('GetLocalFavorites')).length,1);
    assert.ok((await call('ListLocalHistory',200,0)).some(h=>h.historyEntryId==='seed-history'));
    report.offlineRestored=true;report.playlistId=p.playlistId;report.trackIDs=p.tracks.map(t=>t.neteaseSongId);
    report.queueEntries=(await call('LoadQueue')).entries.length;assert.equal(report.queueEntries,4);
    await writeFile('output/playwright/local-playlists-restart-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report));
  } else {
    const baseline=await call('LoadQueue');assert.equal(baseline.revision,7);assert.equal(baseline.positionMs,81000);
    assert.equal((await call('GetSettings')).gdEnabled,true);assert.equal((await call('GetLocalFavorites')).length,1);
    assert.ok((await call('ListLocalHistory',200,0)).some(h=>h.historyEntryId==='seed-history'));assert.equal((await list()).length,0);
    report.schema2DataPreserved=true;
    await page.reload();await page.getByRole('button',{name:'播放',exact:true}).waitFor();
    await page.evaluate(()=>{const play=HTMLMediaElement.prototype.play;window.__playlistPlayers=new Set();HTMLMediaElement.prototype.play=function(...args){this.muted=true;window.__playlistAudio=this;window.__playlistPlayers.add(this);return play.apply(this,args);};});
    await openList();await playlists().getByLabel('新歌单名称').fill('原生歌单A');await playlists().getByRole('button',{name:'创建歌单',exact:true}).click();
    const a=await until(()=>find('原生歌单A'));report.playlistId=a.playlistId;
    await playlists().getByLabel('新歌单名称').fill('待删除歌单');await playlists().getByRole('button',{name:'创建歌单',exact:true}).click();await until(()=>find('待删除歌单'));
    await page.getByRole('button',{name:'收藏歌曲',exact:true}).click();
    await page.getByRole('button',{name:'加入歌单：情非得已 (童声版)',exact:true}).click();
    const dialog=()=>page.getByRole('dialog',{name:'加入歌单',exact:true});await dialog().waitFor();
    for(const key of ['Shift+Tab','Tab','Tab']){await page.keyboard.press(key);assert.equal(await page.evaluate(()=>!!document.activeElement.closest('.playlist-dialog-layer')),true);}
    await dialog().getByRole('button',{name:'原生歌单A · 0 首',exact:true}).click();await dialog().waitFor({state:'hidden'});
    const first=await call('GetLocalPlaylist',a.playlistId);assert.equal(first.tracks.length,1);
    await page.getByRole('button',{name:'加入歌单：情非得已 (童声版)',exact:true}).click();await dialog().getByRole('button',{name:'原生歌单A · 1 首',exact:true}).click();
    await dialog().getByRole('alert').filter({hasText:'这首歌曲已在歌单中'}).waitFor();await page.keyboard.press('Escape');
    assert.equal((await call('GetLocalPlaylist',a.playlistId)).revision,first.revision);report.duplicateProtected=true;
    await page.getByLabel('搜索歌曲或歌手').fill('又三郎');await page.locator('header form').getByRole('button',{name:'搜索',exact:true}).click();
    const song=page.locator('main .music-card').filter({has:page.getByRole('button',{name:'播放 又三郎 - ヨルシカ',exact:true})}).first();await song.waitFor();
    await song.getByRole('button',{name:'加入歌单：又三郎',exact:true}).click();await dialog().getByRole('button',{name:'原生歌单A · 1 首',exact:true}).click();await dialog().waitFor({state:'hidden'});
    assert.deepEqual((await call('GetLocalPlaylist',a.playlistId)).tracks.map(t=>t.neteaseSongId),['33894312','1850977722']);
    await song.getByRole('button',{name:'加入歌单：又三郎',exact:true}).click();await dialog().getByLabel('新歌单名称').fill('重启保留歌单');await dialog().getByRole('button',{name:'创建并加入',exact:true}).click();await dialog().waitFor({state:'hidden'});
    const c=await until(()=>find('重启保留歌单'));report.retainedPlaylistId=c.playlistId;
    assert.equal((await call('LoadQueue')).revision,7);report.playlistEditsLeaveQueueUntouched=true;
    await openList();await openPlaylist('原生歌单A');await detail().getByRole('button',{name:'上移：又三郎',exact:true}).click();
    await until(async()=>(await call('GetLocalPlaylist',a.playlistId)).tracks[0].neteaseSongId==='1850977722');
    await detail().getByRole('button',{name:'下移：又三郎',exact:true}).click();await until(async()=>(await call('GetLocalPlaylist',a.playlistId)).tracks[0].neteaseSongId==='33894312');
    await detail().getByRole('button',{name:'从歌单移除：又三郎',exact:true}).click();await until(async()=>(await call('GetLocalPlaylist',a.playlistId)).tracks.length===1);
    await detail().getByRole('button',{name:'返回歌单',exact:true}).click();await openPlaylist('重启保留歌单');await detail().getByRole('button',{name:'加入歌单：又三郎',exact:true}).click();await dialog().getByRole('button',{name:'原生歌单A · 1 首',exact:true}).click();await dialog().waitFor({state:'hidden'});
    await detail().getByRole('button',{name:'返回歌单',exact:true}).click();await openPlaylist('原生歌单A');
    const current=await call('GetLocalPlaylist',a.playlistId);await call('UpdateLocalPlaylist',{playlistId:a.playlistId,expectedRevision:current.revision,name:'外部编辑名称',tracks:current.tracks});
    await detail().getByLabel('歌单新名称').fill('陈旧修改');await detail().getByRole('button',{name:'重命名',exact:true}).click();await detail().getByRole('alert').filter({hasText:'歌单已被修改或删除'}).waitFor();assert.equal((await call('GetLocalPlaylist',a.playlistId)).name,'外部编辑名称');report.staleCASProtected=true;
    await detail().getByRole('button',{name:'返回歌单',exact:true}).click();await openPlaylist('外部编辑名称');await detail().getByLabel('歌单新名称').fill('原生歌单已重命名');await detail().getByRole('button',{name:'重命名',exact:true}).click();await detail().getByRole('heading',{name:'原生歌单已重命名',exact:true}).waitFor();
    await detail().getByRole('button',{name:'播放全部',exact:true}).click();await page.waitForFunction(()=>window.__playlistAudio?.currentTime>1&&!window.__playlistAudio.paused,undefined,{timeout:20000});
    let queue=await call('LoadQueue');assert.deepEqual(queue.entries.map(e=>e.track.neteaseSongId),['33894312','1850977722']);const selected=queue.currentEntryId;
    const source=await page.evaluate(()=>window.__playlistAudio.currentSrc);
    await detail().getByRole('button',{name:'全部加入队列',exact:true}).click();await until(async()=>(await call('LoadQueue')).entries.length===4);
    queue=await call('LoadQueue');assert.equal(new Set(queue.entries.map(e=>e.queueEntryId)).size,4);assert.equal(queue.currentEntryId,selected);assert.equal(await page.evaluate(()=>window.__playlistAudio.currentSrc),source);
    await detail().getByRole('button',{name:'删除歌单',exact:true}).click();await detail().getByRole('button',{name:'确认删除',exact:true}).click();await playlists().waitFor();assert.ok(!(await list()).some(p=>p.playlistId===a.playlistId));
    assert.equal((await call('LoadQueue')).entries.length,4);assert.equal(await page.evaluate(()=>window.__playlistAudio.currentSrc),source);assert.equal(await page.evaluate(()=>window.__playlistAudio.paused),false);assert.equal(await page.evaluate(()=>window.__playlistPlayers.size),1);
    report.realPlayAll=true;report.appendWithoutInterrupt=true;report.deleteDoesNotAffectPlayback=true;report.singleAudio=true;
    await page.getByRole('button',{name:'暂停',exact:true}).click();await openPlaylist('待删除歌单');await detail().getByRole('button',{name:'删除歌单',exact:true}).click();await detail().getByRole('button',{name:'确认删除',exact:true}).click();await playlists().waitFor();
    assert.equal((await list()).length,1);assert.equal((await call('GetLocalFavorites')).length,1);assert.ok((await call('ListLocalHistory',200,0)).some(h=>h.historyEntryId==='seed-history'));
    report.orderedTracksAndCRUD=true;report.pass=true;await writeFile('output/playwright/local-playlists-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
  }
} catch(error) {report.pass=false;report.failure=error.name;await writeFile('output/playwright/local-playlists-failure.json',JSON.stringify(report,null,2));throw error;
} finally {await browser.close();}
