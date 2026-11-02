import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { chromium } from '../.tools/browser/node_modules/playwright/index.mjs';

const mod = (await readFile('go.mod', 'utf8')).match(/^module (.+)/m)[1].trim();
const binding = await readFile(`frontend/bindings/${mod}/internal/appservice/appservice.ts`, 'utf8');
const id = name => Number(binding.match(new RegExp(`function ${name}\\([^]*?ByID\\((\\d+)`))[1]);
const browser = await chromium.connectOverCDP('http://127.0.0.1:9225');
const page = browser.contexts()[0].pages().find(p => p.url().startsWith('http://wails.localhost/'));
assert.ok(page, 'native application page');
const call = (name, ...args) => page.evaluate(async ({ id, args }) =>
  (await import('/wails/runtime.js')).Call.ByID(id, ...args), { id: id(name), args });

if (process.argv.includes('--verify')) {
  const q = await call('LoadQueue'), s = await call('GetSettings');
  const f = await call('GetLocalFavorites'), h = await call('ListLocalHistory', 200, 0);
  assert.equal(q.positionMs, 124000);
  assert.equal(q.entries.length, 2);
  assert.equal(s.volume, 0.37);
  assert.equal(f.length, 1);
  assert.ok(h.length >= 3);
  assert.equal(await page.getByRole('button', { name: '播放', exact: true }).count(), 1);
  const report = { queueEntries: q.entries.length, positionMs: q.positionMs, volume: s.volume, favorites: f.length, history: h.length, restoredPaused: true, offlineProcessProxy: true };
  await writeFile('output/playwright/native-restart-report.json', JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
  await browser.close();
  process.exit();
}

const targetPID = Number(process.argv.find(arg => arg.startsWith('--pid='))?.slice(6) ?? process.env.OTONEIGO_NATIVE_PID);
assert.ok(Number.isSafeInteger(targetPID) && targetPID > 0, 'explicit --pid or OTONEIGO_NATIVE_PID is required');
// A separately imported /wails/runtime.js installs a second event listener map.
// Reload restores the bundled application dispatcher; do not import it below.
await page.reload();
await page.getByRole('button', { name: '播放', exact: true }).waitFor();
let prepareCount = 0, acknowledgementCount = 0, delayed = false;
await page.exposeFunction('__observeNativeClose', () => { prepareCount++; });
await page.evaluate(() => {
  const dispatch = window._wails.dispatchWailsEvent;
  window._wails.dispatchWailsEvent = function (event) {
    if (event.name === 'otoneigo:prepare-close') void window.__observeNativeClose();
    return dispatch(event);
  };
});
page.on('request', request => {
  if (!request.url().endsWith('/wails/runtime')) return;
  if (request.postDataJSON()?.args?.name === 'otoneigo:close-ready') acknowledgementCount++;
});
await page.route('**/wails/runtime', async route => {
  const body = route.request().postDataJSON();
  if (!delayed && body?.args?.methodID === id('SaveQueue')) {
    delayed = true;
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  await route.continue();
});
const slider = page.getByRole('slider', { name: '播放进度' });
await slider.fill('123000');
await slider.fill('124000');
await page.getByRole('slider', { name: '音量', exact: true }).fill('0.37');
// Enumerate by explicit process ID: Wails' hidden main-thread window can make
// Process.MainWindowHandle zero. Require exactly one titled application window.
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
let closed;
const done = new Promise(resolve => { closed = resolve; });
page.once('close', closed);
// Include PowerShell startup and window discovery in the conservative bound.
const start = Date.now();
await promisify(execFile)('powershell', ['-NoProfile', '-Command', closeCommand], { windowsHide: true });
let timeout;
try {
  await Promise.race([done, new Promise((_, reject) => { timeout = setTimeout(() => reject(Error('CLOSE_TIMEOUT')), 6000); })]);
} finally { clearTimeout(timeout); }
const elapsed = Date.now() - start;
assert.ok(delayed, 'real save request was delayed');
assert.equal(prepareCount, 1);
assert.equal(acknowledgementCount, 1);
assert.ok(elapsed >= 700 && elapsed < 2500, `expected acknowledgement before fallback; took ${elapsed}ms`);
const report = { passThroughSaveRequestDelayMs: 1000, closedAfterMs: elapsed, prepareCount, acknowledgementCount, controlledSaveTransportDelay: true, realSQLite: true, osClose: true };
await writeFile('output/playwright/native-close-report.json', JSON.stringify(report, null, 2));
console.log(JSON.stringify(report));
await browser.close();
