// Live native overseas policy acceptance. No request interception, screenshots,
// synthetic country detection, stream URL output, or host lifecycle actions.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { nativeHarness, until, wait } from './native-harness.mjs';

const output = 'output/playwright/native-egress04-report.json';
const logPath = 'output/native-library-04/log/playback.jsonl';
const report = { nativeWebView2: true, liveRPC: true, networkInterception: false, screenshots: false, simulatedRegion: false };
await mkdir('output/playwright', { recursive: true });
const h = await nativeHarness(), { page, call } = h;
let originalQueue, originalSettings, queueChanged = false, settingsChanged = false;
const load = () => call('LoadQueue');
async function pause() {
  const button = page.getByRole('button', { name: '暂停', exact: true });
  if (await button.count()) {
    const previous = await load(); await button.click();
    await until(async () => (await load()).revision > previous.revision);
  }
  await page.getByRole('button', { name: '播放', exact: true }).waitFor();
}
async function events() {
  try {
    return (await readFile(logPath, 'utf8')).split('\n').filter(Boolean).map(line => {
      const e = JSON.parse(line);
      // Retain only the recorder's public, fixed playback metadata in memory.
      return { stage: e.stage, songId: e.songId, provider: e.provider, status: e.status, strategy: e.strategy, actualBitrate: e.actualBitrate, actualFormat: e.actualFormat };
    });
  } catch (e) { if (e.code === 'ENOENT') return []; throw e; }
}
async function delta(start, songId) {
  const all = await events(); assert.ok(all.length >= start, 'DIAGNOSTIC_LOG_ROTATED');
  return all.slice(start).filter(e => e.songId === songId);
}
function counts(records) {
  return Object.fromEntries(['normal-api', 'compatible-api', 'gd-api', 'cache-hit'].map(stage => [stage, records.filter(e => e.stage === stage).length]));
}
async function audibleClock(duration, title) {
  await page.waitForFunction(({ duration, title }) => {
    const audio = window.__egress04Audio;
    return audio && !audio.paused && audio.currentTime > 0.5 && Math.abs(audio.duration - duration) < 2 && document.querySelector('footer .track-name')?.textContent === title;
  }, { duration, title }, { timeout: 40000 });
  const state = await page.evaluate(() => ({ timeSeconds: window.__egress04Audio.currentTime, durationSeconds: window.__egress04Audio.duration, audioExecutors: window.__egress04Players.size }));
  assert.equal(state.audioExecutors, 1); return state;
}
try {
  report.phase = 'fixture-guard'; originalQueue = await load();
  assert.deepEqual(originalQueue.entries.map(e => e.queueEntryId), ['v04-a', 'v04-b']);
  assert.ok(originalQueue.entries.every(e => e.track.neteaseSongId === '1850977722' && e.track.name === 'OTONEIGO 0.4 原生验收'));
  assert.equal((await call('GetLocalPlaylist', 'v04-playlist')).tracks[0].neteaseSongId, '1850977722');
  const favoriteCount = (await call('GetLocalFavoriteIDs')).length;
  const keywordCount = (await call('ListSearchHistory', 100)).length;
  assert.equal(favoriteCount, 6010);
  originalSettings = await call('GetSettings');
  if (originalSettings.regionMode !== 'auto' || originalSettings.gdEnabled !== true) {
    settingsChanged = true; await call('SetSettings', { ...originalSettings, regionMode: 'auto', gdEnabled: true });
  }
  const settings = await call('GetSettings'); assert.equal(settings.regionMode, 'auto'); assert.equal(settings.gdEnabled, true);
  report.phase = 'real-region';
  const regionDeadline = Date.now() + 15000;
  await Promise.race([call('RefreshRegion'), wait(15000).then(() => { throw Error('REGION_REFRESH_TIMEOUT'); })]);
  const region = await until(async () => { const value = await call('GetRegionStatus'); return value.classification === 'overseas' && value.country === 'TH' && value; }, Math.max(1, regionDeadline - Date.now()));
  report.region = { classification: region.classification, country: region.country };
  report.phase = 'cold-cache-setup'; await pause(); await h.reload();
  await call('ClearAudioCache'); await until(async () => { const s = await call('GetAudioCacheStats'); return s.cachedTracks === 0 && s.activeDownloads === 0 && s.usedBytes === 0; });
  const current = await load(); queueChanged = true;
  await call('SaveQueue', {
    ...current,
    entries: [
      { queueEntryId: 'v04-a', track: { neteaseSongId: '33894312', name: 'OTONEIGO 海外直连策略验收', artists: [{ name: '群星' }], durationMs: 267232 } },
      { queueEntryId: 'v04-b', track: originalQueue.entries[1].track },
    ], currentEntryId: 'v04-a', currentIndex: 0, positionMs: 0, revision: current.revision + 1,
  });
  await h.reload();
  await page.evaluate(() => {
    const original = HTMLMediaElement.prototype.play;
    window.__egress04Players = new Set();
    HTMLMediaElement.prototype.play = function (...args) {
      this.muted = true; window.__egress04Audio = this; window.__egress04Players.add(this);
      return original.apply(this, args);
    };
  });
  report.phase = 'compatible-direct-play';
  const directStart = (await events()).length, directWall = Date.now();
  await page.getByRole('button', { name: '播放', exact: true }).click();
  const directMedia = await audibleClock(267.232, 'OTONEIGO 海外直连策略验收');
  assert.match(await page.locator('footer .stream-source').textContent(), /网易云直连/);
  const directEvents = await delta(directStart, '33894312'), directCounts = counts(directEvents);
  assert.equal(directCounts['normal-api'], 0); assert.ok(directCounts['compatible-api'] >= 1); assert.equal(directCounts['gd-api'], 0); assert.equal(directCounts['cache-hit'], 0);
  const direct = directEvents.filter(e => e.stage === 'compatible-api').at(-1);
  assert.equal(direct.provider, 'netease-direct'); assert.equal(direct.status, 'full'); assert.equal(direct.strategy, 'region-header'); assert.equal(direct.actualBitrate, 320000);
  report.direct = { provider: direct.provider, status: direct.status, strategy: direct.strategy, actualBitrate: direct.actualBitrate, startupWallMs: Date.now() - directWall, ...directMedia, attempts: directCounts };
  report.phase = 'independent-gd-fallback-play';
  const gdStart = (await events()).length, gdWall = Date.now();
  await page.getByRole('button', { name: '下一首', exact: true }).click();
  const gdMedia = await audibleClock(227.760, 'OTONEIGO 0.4 原生验收');
  assert.match(await page.locator('footer .stream-source').textContent(), /GD 网易云备用音源/);
  const gdEvents = await delta(gdStart, '1850977722'), gdCounts = counts(gdEvents);
  assert.equal(gdCounts['normal-api'], 0); assert.ok(gdCounts['compatible-api'] >= 1); assert.equal(gdCounts['gd-api'], 1); assert.equal(gdCounts['cache-hit'], 0);
  const gd = gdEvents.find(e => e.stage === 'gd-api'); assert.equal(gd.provider, 'gd-netease'); assert.equal(gd.status, 'full');
  report.gd = { provider: gd.provider, status: gd.status, startupWallMs: Date.now() - gdWall, ...gdMedia, attempts: gdCounts };
  assert.equal((await call('GetLocalFavoriteIDs')).length, favoriteCount);
  assert.equal((await call('ListSearchHistory', 100)).length, keywordCount);
  report.libraryAndKeywordsPreserved = true; report.singleAudioExecutor = true;
  report.pass = true;
} catch (error) {
  report.pass = false;
  report.failure = error instanceof assert.AssertionError ? 'ASSERTION_FAILED' : report.phase === 'real-region' ? 'REAL_OVERSEAS_CONDITION_NOT_PASSED' : 'NATIVE_CHECK_FAILED';
  report.failureLocation = error.stack?.match(/native-egress04-acceptance\.mjs:\d+:\d+/)?.[0] ?? 'NATIVE_HELPER';
} finally {
  try {
    if (queueChanged) {
      await pause(); const current = await load();
      await call('SaveQueue', { ...originalQueue, revision: current.revision + 1 }); await h.reload();
      const restored = await load();
      assert.deepEqual(restored.entries, originalQueue.entries); assert.equal(restored.positionMs, originalQueue.positionMs); assert.equal(restored.currentEntryId, originalQueue.currentEntryId);
      report.originalQueueRestoredPaused = true;
    }
    if (settingsChanged) { await call('SetSettings', originalSettings); assert.deepEqual(await call('GetSettings'), originalSettings); }
    report.settingsPreserved = true;
  } catch {
    report.pass = false; report.cleanupFailure = 'FIXTURE_RESTORE_FAILED';
  }
  await writeFile(output, JSON.stringify(report, null, 2)); console.log(JSON.stringify(report, null, 2));
  await h.browser.close(); // Disconnect only. The caller owns the native process.
  if (!report.pass) process.exitCode = 1;
}
