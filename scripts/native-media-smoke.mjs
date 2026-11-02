// Controlled media acceptance in the actual Wails WebView2 renderer.
// The resolver and lyrics are fixtures; SQLite, React and AudioEngine are real.
// This is NOT evidence of live Netease stream availability.
import assert from 'node:assert/strict';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from '../.tools/browser/node_modules/playwright/index.mjs';

const binding = await readFile(new URL('../frontend/bindings/github.com/voici5986/otoneigo/internal/appservice/appservice.ts', import.meta.url), 'utf8');
const methodID = (name) => Number(binding.match(new RegExp(`function ${name}\\([^]*?ByID\\((\\d+)`))[1]);
const resolveID = methodID('ResolveStream');
const lyricsID = methodID('GetLyrics');
const loadID = methodID('LoadQueue');
const output = new URL('../output/playwright/', import.meta.url);
await mkdir(output, { recursive: true });

// Thirty seconds of PCM silence: exercises decoding and timing without sound.
const pcm = Buffer.alloc(44 + 8000 * 2 * 30);
pcm.write('RIFF', 0); pcm.writeUInt32LE(pcm.length - 8, 4); pcm.write('WAVEfmt ', 8);
pcm.writeUInt32LE(16, 16); pcm.writeUInt16LE(1, 20); pcm.writeUInt16LE(1, 22);
pcm.writeUInt32LE(8000, 24); pcm.writeUInt32LE(16000, 28); pcm.writeUInt16LE(2, 32);
pcm.writeUInt16LE(16, 34); pcm.write('data', 36); pcm.writeUInt32LE(pcm.length - 44, 40);
let browser;
for (let attempt = 0; attempt < 40; attempt++) {
  try {
    browser = await chromium.connectOverCDP('http://127.0.0.1:9225');
    break;
  } catch (error) {
    if (attempt === 39) throw error;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
}
const page = browser.contexts()[0].pages().find(p => p.url().startsWith('http://wails.localhost/'));
assert.ok(page, 'Native Wails page must be running');
const report = { kind: 'controlled-fixture-in-native-webview2', liveNeteasePlayback: false };
let resolveCount = 0;
let responseDelayMs = 0;
await page.route('https://p0-fixture.music.126.net/**', route => {
  const range = route.request().headers().range?.match(/^bytes=(\d+)-(\d*)$/);
  const start = range ? Number(range[1]) : 0;
  const end = range && range[2] ? Math.min(Number(range[2]), pcm.length - 1) : pcm.length - 1;
  return route.fulfill({ status: range ? 206 : 200, contentType: 'audio/wav',
    headers: { 'Accept-Ranges': 'bytes', ...(range ? { 'Content-Range': `bytes ${start}-${end}/${pcm.length}` } : {}) },
    body: pcm.subarray(start, end + 1),
  });
});
await page.route('**/wails/runtime', async route => {
  const body = route.request().postDataJSON();
  const call = body?.args;
  if (body?.object === 0 && call?.methodID === resolveID) {
    resolveCount++;
    if (responseDelayMs) await new Promise(resolve => setTimeout(resolve, responseDelayMs));
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      songId: call.args[0], provider: 'netease-direct',
      url: `https://p0-fixture.music.126.net/${call.args[0]}.wav`,
      requestedQuality: call.args[1], actualFormat: 'wav', status: 'unknown', durationMs: 30000,
    }) });
  } else if (body?.object === 0 && call?.methodID === lyricsID) {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ original: '[00:00.00]P0 media test fixture', translated: '' }) });
  } else await route.continue();
});
try {
  await page.getByRole('button', { name: '播放队列', exact: true }).click();
  assert.ok(await page.locator('.queue-drawer li').count() >= 2, 'Add at least two queue entries with real search before this test');
  await page.getByRole('button', { name: '关闭队列', exact: true }).click();
  const readSaved = () => page.evaluate(async id => (await import('/wails/runtime.js')).Call.ByID(id), loadID);
  const before = await readSaved();
  report.startingRevision = before.revision;
  await page.evaluate(() => {
    const original = HTMLMediaElement.prototype.play;
    window.__p0Players = new Set();
    HTMLMediaElement.prototype.play = function (...args) {
      window.__p0Players.add(this);
      window.__p0Audio = this;
      if (!this.__p0Observed) {
        this.__p0Observed = true;
        window.__p0MediaEvents = [];
        for (const type of ['loadedmetadata', 'seeking', 'seeked', 'playing', 'pause', 'error'])
          this.addEventListener(type, () => window.__p0MediaEvents.push({ type, time: this.currentTime, ready: this.readyState }));
      }
      return original.apply(this, args);
    };
  });
  await page.getByRole('button', { name: '播放', exact: true }).click();
  await page.waitForFunction(() => window.__p0Audio?.currentTime > 0.5 && !window.__p0Audio.paused, undefined, { timeout: 15000 });
  report.decodedDurationSeconds = await page.evaluate(() => window.__p0Audio.duration);
  assert.equal(report.decodedDurationSeconds, 30);
  await page.getByRole('button', { name: '暂停', exact: true }).click();
  assert.equal(await page.evaluate(() => window.__p0Audio.paused), true);
  const slider = page.getByRole('slider', { name: '播放进度' });
  await slider.press('Home');
  for (let step = 0; step < 10; step++) await slider.press('ArrowRight');
  assert.equal(await slider.inputValue(), '10000');
  let seekSaved;
  const saveDeadline = Date.now() + 5000;
  do {
    seekSaved = await readSaved();
    if (seekSaved.positionMs === 10000 && seekSaved.revision > before.revision) break;
    await new Promise(resolve => setTimeout(resolve, 100));
  } while (Date.now() < saveDeadline);
  assert.equal(seekSaved.positionMs, 10000, 'Actual Go/SQLite binding must save nonzero seek position');
  assert.ok(seekSaved.revision > before.revision, 'An earlier saved snapshot cannot pass persistence acceptance');
  report.persistedSeekPositionMs = seekSaved.positionMs;
  report.persistedSeekRevision = seekSaved.revision;
  await page.getByRole('button', { name: '播放', exact: true }).click();
  // Timeout is shorter than ten seconds: natural playback cannot satisfy this.
  await page.waitForFunction(() => window.__p0Audio.currentTime >= 10 && window.__p0Audio.currentTime < 12 && !window.__p0Audio.paused, undefined, { timeout: 5000 });
  report.resumedMediaPositionSeconds = await page.evaluate(() => window.__p0Audio.currentTime);
  report.seekPassed = true;
  await page.screenshot({ path: fileURLToPath(new URL('native-fixture-playing.png', output)) });
  await page.getByRole('button', { name: '暂停', exact: true }).click();
  await page.getByRole('button', { name: '下一首', exact: true }).click();
  await page.waitForFunction(() => window.__p0Audio.currentTime > 0.3 && !window.__p0Audio.paused);
  assert.equal(await page.evaluate(() => window.__p0Players.size), 1);
  report.singleAudioExecutor = true;
  await page.getByRole('button', { name: '暂停', exact: true }).click();
  responseDelayMs = 800;
  const countBeforeCancel = resolveCount;
  await page.getByRole('button', { name: '下一首', exact: true }).click();
  await page.getByRole('button', { name: '取消加载', exact: true }).click();
  await page.waitForTimeout(1100);
  assert.equal(await page.evaluate(() => window.__p0Audio.paused), true);
  assert.ok(resolveCount > countBeforeCancel);
  report.cancelledLateResultRemainedPaused = true;
  // Use actual generated binding ID; no library data is mocked.
  const saved = await readSaved();
  assert.equal(saved.entries.length, 2);
  assert.ok(Number.isSafeInteger(saved.positionMs));
  assert.ok(saved.revision > seekSaved.revision);
  assert.equal(new Set(saved.entries.map(e => e.queueEntryId)).size, 2);
  assert.ok(!JSON.stringify(saved).includes('p0-fixture'));
  report.persistedRevision = saved.revision;
  report.persistedPositionIsInteger = true;
  report.fixtureURLNotPersisted = true;
  await writeFile(new URL('native-media-report.json', output), JSON.stringify(report, null, 2));
  process.stdout.write(JSON.stringify(report, null, 2) + '\n');
} catch (error) {
  const state = await page.evaluate(() => ({
    time: window.__p0Audio?.currentTime, paused: window.__p0Audio?.paused,
    ready: window.__p0Audio?.readyState, events: window.__p0MediaEvents,
    alerts: Array.from(document.querySelectorAll('[role=alert]')).map(e => e.textContent),
  }));
  process.stdout.write(JSON.stringify({ failedNativeCheck: state }, null, 2) + '\n');
  throw error;
} finally {
  const pause = page.getByRole('button', { name: '暂停', exact: true });
  if (await pause.count()) await pause.click();
  await page.unrouteAll({ behavior: 'wait' });
  await browser.close(); // CDP disconnect; the host app remains alive.
}
