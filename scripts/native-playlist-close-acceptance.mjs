// Native playlist flush acceptance; delay the real update, then post WM_CLOSE.
import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {chromium} from '../.tools/browser/node_modules/playwright/index.mjs';
const targetPID=Number(process.argv.find(a=>a.startsWith('--pid='))?.slice(6));assert.ok(Number.isSafeInteger(targetPID)&&targetPID>0);
const mod=(await readFile('go.mod','utf8')).match(/^module (.+)/m)[1].trim();
const binding=await readFile(`frontend/bindings/${mod}/internal/appservice/appservice.ts`,'utf8');
const updateID=Number(binding.match(/function UpdateLocalPlaylist\([^]*?ByID\((\d+)/)[1]);
const browser=await chromium.connectOverCDP('http://127.0.0.1:9225');const page=browser.contexts()[0].pages()[0];
await page.reload();await page.getByRole('button',{name:'播放',exact:true}).waitFor();
let prepare=0,ack=0,delayed=false;
await page.exposeFunction('__observePlaylistClose',()=>{prepare++;});
await page.evaluate(()=>{const original=window._wails.dispatchWailsEvent;window._wails.dispatchWailsEvent=function(event){if(event.name==='otoneigo:prepare-close')void window.__observePlaylistClose();return original(event);};});
page.on('request',r=>{if(r.url().endsWith('/wails/runtime')&&r.postDataJSON()?.args?.name==='otoneigo:close-ready')ack++;});
await page.route('**/wails/runtime',async route=>{if(!delayed&&route.request().postDataJSON()?.args?.methodID===updateID){delayed=true;await new Promise(r=>setTimeout(r,1000));}await route.continue();});
await page.getByRole('navigation',{name:'主导航'}).getByRole('button',{name:'本地收藏',exact:true}).click();await page.getByRole('button',{name:'本地歌单',exact:true}).click();
await page.getByRole('region',{name:'本地歌单',exact:true}).getByRole('button').filter({has:page.getByRole('heading',{name:'重启保留歌单',exact:true})}).click();
const details=page.getByRole('region',{name:'本地歌单详情',exact:true});await details.getByLabel('歌单新名称').fill('关窗保存歌单');await details.getByRole('button',{name:'重命名',exact:true}).click();
const closeCommand = `
Add-Type @"
using System;using System.Runtime.InteropServices;using System.Text;
public class NativeClose {
 public delegate bool Callback(IntPtr h,IntPtr p);
 [DllImport("user32.dll")] public static extern bool EnumWindows(Callback c,IntPtr p);
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
 [DllImport("user32.dll",EntryPoint="PostMessageW")] public static extern bool PostMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
}
"@
$targets=[Collections.Generic.List[IntPtr]]::new()
[NativeClose]::EnumWindows({param($handle,$unused)
 $owner=0;[NativeClose]::GetWindowThreadProcessId($handle,[ref]$owner)|Out-Null
 if($owner -eq ${targetPID}) {
  $title=[Text.StringBuilder]::new(256);[NativeClose]::GetWindowText($handle,$title,256)|Out-Null
  if($title.ToString() -eq 'OTONEIGO') {$targets.Add($handle)}
 }
 return $true
},[IntPtr]::Zero)|Out-Null
if($targets.Count -ne 1){throw 'Expected exactly one OTONEIGO window for the supplied PID'}
if(-not [NativeClose]::PostMessage($targets[0],0x10,[IntPtr]::Zero,[IntPtr]::Zero)){throw 'WM_CLOSE could not be posted'}
`;
let closed;const done=new Promise(r=>{closed=r;});page.once('close',closed);const start=Date.now();
await promisify(execFile)('powershell',['-NoProfile','-Command',closeCommand],{windowsHide:true});let timer;
try{await Promise.race([done,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error('PLAYLIST_CLOSE_TIMEOUT')),6000);})]);}finally{clearTimeout(timer);}
const ms=Date.now()-start;assert.ok(delayed);assert.equal(prepare,1);assert.equal(ack,1);assert.ok(ms>=700&&ms<2500);
const report={native:true,realSQLite:true,controlledUpdateRequestDelayMs:1000,closedAfterMs:ms,prepare,ack,osClose:true,screenshots:false};await writeFile('output/playwright/local-playlists-close-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report));await browser.close();
