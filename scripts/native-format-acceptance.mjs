// Controlled codec acceptance ONLY: real WebView2 decoding and the product's
// single HTMLAudioElement, with fixture ResolveStream/GetLyrics responses.
// NOT evidence of Netease/member availability, cache transport or quality rights.
// Requires an app ALREADY launched with OTONEIGO_DATA_DIR=output/native-cache-desktop.
// Explicit acknowledgement is required because CDP cannot verify host environment.
import assert from 'node:assert/strict';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { nativeHarness } from './native-harness.mjs';

assert.equal(process.argv[2], '--isolated-profile=output/native-cache-desktop',
  'Launch the isolated native-cache-desktop app first, then explicitly acknowledge its profile');
const output = path.resolve('output/playwright');
await mkdir(output, { recursive: true });
const fixtureDir = path.resolve('.tools/musicfox-diagnosis/codec-fixtures');
const expected = JSON.parse(await readFile(path.join(fixtureDir, 'generation-report.json'), 'utf8'));
const inspected = JSON.parse(await readFile(path.join(fixtureDir, 'audioinfo-report.json'), 'utf8'));
const fixtures = {};
for (const format of ['wav', 'flac']) {
  const bytes = await readFile(path.join(fixtureDir, `silence-3s.${format}`));
  assert.equal(createHash('sha256').update(bytes).digest('hex'), expected.files[format].sha256);
  assert.equal(inspected[format].DurationMs, 3000);
  fixtures[format] = { bytes, mime: `audio/${format}`, songId: format === 'wav' ? '99000000001' : '99000000002' };
}
const { browser, page, id, call, reload } = await nativeHarness();
const report = {
  kind: 'controlled-codec-fixtures-in-native-webview2',
  isolatedProfileAcknowledged: 'output/native-cache-desktop',
  nativeDecoder: true, resolverInterception: true, mediaInterception: true,
  liveNeteasePlayback: false, memberPlayback: false, cacheTransport: false,
  muted: true, fixtures: expected.files, outcomes: {},
};
let originalQueue, queueChanged = false, observerInstalled = false;
const mediaPattern = 'https://codec-fixture.music.126.net/**';
const runtimePattern = '**/wails/runtime';
const mediaHandler = async route => {
  const format = new URL(route.request().url()).pathname.slice(1);
  const fixture = fixtures[format];
  if (!fixture) return route.fulfill({ status: 404, body: '' });
  const rangeValue = route.request().headers().range;
  let start = 0, end = fixture.bytes.length - 1;
  if (rangeValue) {
    const range = /^bytes=(\d*)-(\d*)$/.exec(rangeValue);
    if (!range || (!range[1] && !range[2])) return route.fulfill({ status: 416, body: '' });
    if (!range[1]) start = Math.max(0, fixture.bytes.length - Number(range[2]));
    else { start = Number(range[1]); if (range[2]) end = Math.min(end, Number(range[2])); }
    if (start > end || start >= fixture.bytes.length) return route.fulfill({ status: 416,
      headers: { 'Content-Range': `bytes */${fixture.bytes.length}` }, body: '' });
  }
  const bytes = fixture.bytes.subarray(start, end + 1);
  await route.fulfill({ status: rangeValue ? 206 : 200, contentType: fixture.mime,
    headers: { 'Accept-Ranges': 'bytes', 'Content-Length': String(bytes.length),
      ...(rangeValue ? { 'Content-Range': `bytes ${start}-${end}/${fixture.bytes.length}` } : {}) },
    body: bytes });
};
const runtimeHandler = async route => {
  let request;
  try { request = route.request().postDataJSON(); } catch { return route.continue(); }
  const args = request?.args;
  const fixture = Object.entries(fixtures).find(([, f]) => f.songId === args?.args?.[0]);
  if (request?.object !== 0 || !fixture) return route.continue();
  const [format, f] = fixture;
  if (args.methodID === id('ResolveStream')) {
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      songId: f.songId, provider: 'netease-direct', accessStrategy: 'normal',
      url: `https://codec-fixture.music.126.net/${format}`,
      requestedQuality: args.args[1], actualQuality: 'lossless', actualFormat: format,
      actualBitrate: inspected[format].Bitrate, status: 'full', durationMs: 3000,
      contentLengthBytes: f.bytes.length,
    }) });
  }
  if (args.methodID === id('GetLyrics')) return route.fulfill({ status: 200,
    contentType: 'application/json', body: JSON.stringify({ original: '[00:00.00]Controlled codec fixture', translated: '' }) });
  return route.continue();
};
const pause = async () => {
  const button = page.getByRole('button', { name: '暂停', exact: true });
  if (await button.count()) await button.click();
};
try {
  await pause();
  originalQueue = await call('LoadQueue');
  // Guard against accidentally targeting the regular user profile. This marker
  // belongs to the explicitly prepared native-cache-desktop acceptance profile.
  assert.ok(originalQueue?.entries?.some(e => e.track.name.startsWith('OTONEIGO 缓存原生验收')),
    'Expected isolated native-cache-desktop queue marker; refusing to modify this profile');
  await page.route(mediaPattern, mediaHandler);
  await page.route(runtimePattern, runtimeHandler);
  const entries = Object.entries(fixtures).map(([format, f]) => ({
    queueEntryId: `codec-${format}`, track: { neteaseSongId: f.songId,
      name: `Controlled ${format.toUpperCase()} codec fixture`, artists: [{ name: 'Generated PCM silence' }], durationMs: 3000 },
  }));
  await call('SaveQueue', { entries, currentEntryId: entries[0].queueEntryId, currentIndex: 0,
    playMode: 'repeat-all', positionMs: 0, revision: originalQueue.revision + 1 });
  queueChanged = true;
  await reload();
  await page.evaluate(() => {
    const originalPlay = HTMLMediaElement.prototype.play;
    const state = window.__codecAcceptance = { originalPlay, players: new Set(), results: {} };
    // Observation only: invoke the original browser play implementation on the
    // same product-created element. No executor replacement or fake events.
    HTMLMediaElement.prototype.play = function (...args) {
      if (new URL(this.src).hostname !== 'codec-fixture.music.126.net') return originalPlay.apply(this, args);
      state.players.add(this); this.muted = true;
      const format = new URL(this.src).pathname.slice(1);
      const result = state.results[format] ??= { durationSeconds: this.duration, maximumTimeSeconds: 0, timeupdateCount: 0, ended: false, mediaErrors: [] };
      if (!this.__codecObserved) {
        this.__codecObserved = true;
        this.addEventListener('timeupdate', () => {
          if (!this.src) return;
          const record = state.results[new URL(this.src).pathname.slice(1)];
          if (record) { record.maximumTimeSeconds = Math.max(record.maximumTimeSeconds, this.currentTime); record.timeupdateCount++; }
        });
        this.addEventListener('ended', () => {
          const record = state.results[new URL(this.src).pathname.slice(1)];
          if (record && this.ended) { record.ended = true; record.endedAtSeconds = this.currentTime; record.durationSeconds = this.duration; }
        }, { capture: true });
        this.addEventListener('error', () => {
          if (!this.src) return;
          state.results[new URL(this.src).pathname.slice(1)]?.mediaErrors.push(this.error?.code ?? 0);
        });
      }
      return originalPlay.apply(this, args);
    };
  });
  observerInstalled = true;
  await page.getByRole('button', { name: '播放', exact: true }).click();
  // Both fixtures must decode and reach a real natural ended event. No seeks,
  // playbackRate adjustments, synthetic media events or direct element creation.
  await page.waitForFunction(() => ['wav', 'flac'].every(f => window.__codecAcceptance.results[f]?.ended),
    undefined, { timeout: 20000 });
  await pause();
  const observed = await page.evaluate(() => ({ players: window.__codecAcceptance.players.size,
    results: window.__codecAcceptance.results }));
  assert.equal(observed.players, 1, 'The same actual HTMLAudioElement must play both codecs');
  report.singleAudioExecutor = true;
  for (const format of ['wav', 'flac']) {
    const result = observed.results[format];
    assert.ok(Math.abs(result.durationSeconds - 3) < 0.02, `${format}: decoded duration`);
    assert.ok(result.maximumTimeSeconds > 2.8 && result.timeupdateCount > 1, `${format}: actual media clock advanced`);
    assert.ok(result.ended && result.endedAtSeconds >= 2.99, `${format}: natural ended`);
    assert.deepEqual(result.mediaErrors, [], `${format}: no media error`);
    report.outcomes[format] = result;
  }
  report.passed = true;
} catch (error) {
  report.passed = false;
  // Deliberately exclude DOM text, source URLs, cookies, tokens and network logs.
  report.failureCode = error.name === 'TimeoutError' ? 'NATIVE_CODEC_TIMEOUT' : 'NATIVE_CODEC_ASSERTION';
  if (observerInstalled) report.outcomes = await page.evaluate(() => window.__codecAcceptance.results);
  throw error;
} finally {
  try {
    await pause();
    if (observerInstalled) await page.evaluate(() => {
      HTMLMediaElement.prototype.play = window.__codecAcceptance.originalPlay;
    });
    if (queueChanged) {
      const current = await call('LoadQueue');
      await call('SaveQueue', { ...originalQueue, revision: current.revision + 1 });
      await reload();
      report.queueRestored = true;
    }
  } finally {
    await page.unroute(mediaPattern, mediaHandler);
    await page.unroute(runtimePattern, runtimeHandler);
    await writeFile(path.join(output, 'native-format-report.json'), JSON.stringify(report, null, 2) + '\n');
    console.log(JSON.stringify(report, null, 2));
    await browser.close(); // Disconnect CDP; leave the native host alive.
  }
}
