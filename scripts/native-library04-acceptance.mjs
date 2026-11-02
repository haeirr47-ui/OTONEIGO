// Live Wails/WebView2 acceptance; no RPC/media interception or screenshots.
// Host lifecycle and process-only proxy configuration belong to the caller.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { nativeHarness, until, wait } from './native-harness.mjs';

const mode = process.argv[2]?.replace(/^--/, '');
assert.ok(['online', 'offline', 'prepare-crash', 'recovery'].includes(mode), 'EXPLICIT_MODE_REQUIRED');
const profile = 'output/native-library-04';
const output = 'output/playwright';
const songID = '1850977722', targetID = '99000000001';
const report = { mode, nativeWebView2: true, liveRPC: true, networkInterception: false, screenshots: false };
await mkdir(output, { recursive: true });
if (mode === 'online') {
  try {
    const previous = JSON.parse(await readFile(`${output}/library04-online-report.json`, 'utf8'));
    const path = `${output}/library04-online-attempts.json`;
    let attempts = []; try { attempts = JSON.parse(await readFile(path, 'utf8')); } catch (e) { if (e.code !== 'ENOENT') throw e; }
    attempts.push({ recordedAt: new Date().toISOString(), pass: previous.pass, failure: previous.failure, failureLocation: previous.failureLocation, libraryUI: previous.libraryPagingAndWholeDatabaseUnicodeLiteralFilter, searchUI: previous.searchHistoryUI, startupWallMs: previous.startupWallMs });
    await writeFile(path, JSON.stringify(attempts, null, 2));
  } catch (e) { if (e.code !== 'ENOENT') throw e; }
}
const h = await nativeHarness(), { page, call } = h;
const nav = name => page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name, exact: true });
const audioStats = () => call('GetAudioCacheStats');
const resourceStats = () => call('GetResourceCacheStats');
const savedQueue = () => call('LoadQueue');
const history = () => call('ListSearchHistory', 100);
const ids = () => call('GetLocalFavoriteIDs');
const digest = value => { const text = JSON.stringify(value); return { sha256: createHash('sha256').update(text).digest('hex'), bytes: Buffer.byteLength(text) }; };
const publicQueue = q => ({ revision: q.revision, positionMs: q.positionMs, currentEntryId: q.currentEntryId, entryIDs: q.entries.map(e => e.queueEntryId), songIDs: q.entries.map(e => e.track.neteaseSongId) });
async function guard() {
  const q = await savedQueue();
  assert.deepEqual(q.entries.map(e => e.queueEntryId), ['v04-a', 'v04-b'], 'ISOLATED_FIXTURE_REQUIRED');
  assert.ok(q.entries.every(e => e.track.neteaseSongId === songID && e.track.name === 'OTONEIGO 0.4 原生验收'), 'ISOLATED_FIXTURE_REQUIRED');
  assert.equal((await call('GetLocalPlaylist', 'v04-playlist')).tracks[0].neteaseSongId, songID);
  return q;
}
async function library() {
  const favorites = await ids(); assert.equal(favorites.length, 6010); assert.ok(favorites.includes(songID)); assert.ok(favorites.includes(targetID));
  const tail = await call('ListLocalHistory', 2, 5000); assert.ok(tail.length > 0);
  const settings = await call('GetSettings');
  assert.equal(settings.preferredQuality, 'exhigh'); assert.equal(settings.gdEnabled, true); assert.equal(settings.volume, 0.23);
  assert.equal(settings.audioCache.enabled, true); assert.equal(settings.audioCache.maxBytes, 20 * 1024 ** 3);
  assert.equal(settings.resourceCache.enabled, true); assert.equal(settings.resourceCache.maxBytes, 1024 ** 3);
  report.preserved = { favorites: favorites.length, historyBeyond5000: true, playlistTracks: 1, settings: true };
}
async function instrument() {
  await page.evaluate(() => {
    const original = HTMLMediaElement.prototype.play;
    window.__library04Players = new Set();
    HTMLMediaElement.prototype.play = function (...args) {
      this.muted = true; window.__library04Audio = this; window.__library04Players.add(this);
      return original.apply(this, args);
    };
  });
}
async function pause() {
  const button = page.getByRole('button', { name: '暂停', exact: true });
  if (await button.count()) { const q = await savedQueue(); await button.click(); await until(async () => (await savedQueue()).revision > q.revision); }
}
async function seek(ms, playing = false) {
  const q = await savedQueue();
  await page.getByRole('slider', { name: '播放进度', exact: true }).fill(String(ms));
  const saved = await until(async () => { const next = await savedQueue(); return next.revision > q.revision && next.positionMs >= ms && next.positionMs < ms + (playing ? 3000 : 1) && next; });
  if (playing) await page.waitForFunction(target => window.__library04Audio.currentTime >= target && window.__library04Audio.currentTime < target + 4, ms / 1000, { timeout: 10000 });
  return saved;
}
async function play(timeout = 40000) {
  const start = Date.now(); await page.getByRole('button', { name: '播放', exact: true }).click();
  await page.waitForFunction(() => window.__library04Audio?.currentTime > 0.5 && !window.__library04Audio.paused, undefined, { timeout });
  assert.equal(await page.evaluate(() => window.__library04Players.size), 1);
  assert.equal(new URL(await page.evaluate(() => window.__library04Audio.currentSrc)).hostname, '127.0.0.1');
  return Date.now() - start;
}
async function requestCount() {
  try { return (await readFile(`${profile}/log/playback.jsonl`, 'utf8')).trim().split('\n').filter(Boolean).map(line => JSON.parse(line)).filter(e => ['normal-api', 'compatible-api', 'gd-api'].includes(e.stage)).length; }
  catch (e) { if (e.code === 'ENOENT') return 0; throw e; }
}
async function resources(expected) {
  const track = (await savedQueue()).entries[0].track;
  assert.ok(track.coverRef); const start = Date.now(); const location = await call('ResolveCover', track.coverRef);
  assert.match(location, /^\/artwork\/[0-9a-f]{64}$/);
  const image = await page.evaluate(async location => {
    const response = await fetch(location); if (!response.ok) throw Error('ARTWORK_HTTP_FAILED');
    const bytes = (await response.arrayBuffer()).byteLength;
    const img = new Image(); img.src = location; await img.decode();
    return { bytes, width: img.naturalWidth, height: img.naturalHeight };
  }, location);
  assert.ok(image.bytes > 0 && image.width > 0 && image.height > 0);
  const lyrics = await call('GetLyrics', songID); assert.ok(lyrics.original?.trim()); const signature = digest(lyrics);
  if (expected) assert.deepEqual(signature, expected.lyrics);
  const stats = await resourceStats(); assert.ok(stats.cachedCovers >= 1 && stats.cachedLyrics >= 1);
  return { lyrics: signature, cover: image, wallMs: Date.now() - start, stats };
}
async function retainPosition() { await pause(); const q = await seek(124000); report.committedQueue = publicQueue(q); }
async function localUI() {
  const panels = [['本地收藏', '收藏翻页'], ['播放历史', '历史翻页']];
  for (const [name, pagination] of panels) {
    await nav(name).click(); const panel = page.getByRole('region', { name, exact: true });
    await panel.getByLabel('筛选本地音乐').fill('');
    const cards = panel.locator('.music-card'); await until(async () => (await cards.count()) === 200);
    if (name === '本地收藏') { assert.equal(await cards.filter({ hasText: 'OTONEIGO 0.4 原生验收' }).count(), 0); await page.locator('footer').getByRole('button', { name: '取消收藏：OTONEIGO 0.4 原生验收', exact: true }).waitFor(); }
    const pager = panel.getByRole('navigation', { name: pagination });
    const first = await cards.first().textContent(); await pager.getByRole('button', { name: '下一页', exact: true }).click();
    await until(async () => (await pager.textContent()).includes('第 2 页')); assert.equal(await cards.count(), 200); assert.notEqual(await cards.first().textContent(), first);
    await pager.getByRole('button', { name: '上一页', exact: true }).click(); await until(async () => (await pager.textContent()).includes('第 1 页')); assert.equal(await cards.first().textContent(), first);
    for (const query of ['整库尾部 猫_%', 'ÄLBUM', 'ヨルシカ', '_%']) {
      const method = name === '本地收藏' ? 'SearchLocalFavorites' : 'SearchLocalHistory';
      const result = await call(method, query, 200, 0, '');
      const tracks = result.tracks ?? result.entries.map(entry => entry.track);
      assert.ok(tracks.some(track => track.neteaseSongId === targetID));
      if (query !== 'ヨルシカ') assert.deepEqual(tracks.map(track => track.neteaseSongId), [targetID]);
      await panel.getByLabel('筛选本地音乐').fill(query);
      // Each query must cross the 250 ms debounce even when it finds the same row.
      await wait(400);
      await until(async () => (await cards.count()) === tracks.length && (await cards.filter({ hasText: '整库尾部 猫_%' }).count()) === 1);
    }
    await panel.getByLabel('筛选本地音乐').fill(''); await until(async () => (await cards.count()) === 200);
  }
  report.libraryPagingAndWholeDatabaseUnicodeLiteralFilter = true;
}
async function search(query) {
  await nav('发现音乐').click(); await page.getByLabel('搜索歌曲或歌手').fill(query); await page.getByLabel('搜索歌曲或歌手').press('Enter');
  await until(async () => (await history()).some(entry => entry.query === query), 30000);
  await until(async () => await page.getByRole('region', { name: '音乐搜索', exact: true }).locator('.music-card').count() > 0);
}
async function staleArtworkRecovery() {
  const client = await page.context().newCDPSession(page);
  // Disable only Chromium's HTTP cache so the old URL performs a real 404 read.
  // This neither intercepts requests nor alters the app's native resource cache.
  await client.send('Network.enable'); await client.send('Network.setCacheDisabled', { cacheDisabled: true });
  try {
    await page.waitForFunction(() => /^\/artwork\/[a-f0-9]{64}$/.test(document.querySelector('footer .album-cover-image')?.getAttribute('src') ?? ''));
    await page.evaluate(() => { window.__library04OldImage = document.querySelector('footer .album-cover-image'); window.__library04OldArtwork = window.__library04OldImage.getAttribute('src'); });
    await call('ClearResourceCache'); await until(async () => (await resourceStats()).cachedCovers === 0);
    const status = await page.evaluate(async () => (await fetch(window.__library04OldArtwork, { cache: 'no-store' })).status); assert.equal(status, 404);
    await page.evaluate(() => {
      window.__library04ImageErrors = 0;
      const image = window.__library04OldImage;
      image.addEventListener('error', () => { window.__library04ImageErrors++; });
      image.src = ''; image.src = window.__library04OldArtwork;
    });
    await page.waitForFunction(() => {
      const image = document.querySelector('footer .album-cover-image');
      return window.__library04ImageErrors === 1 && image !== window.__library04OldImage && image?.getAttribute('src') === window.__library04OldArtwork && image.complete && image.naturalWidth > 0;
    }, undefined, { timeout: 20000 });
    await page.locator('footer .album-cover-image').evaluate(image => image.decode());
    assert.ok((await resourceStats()).cachedCovers >= 1); report.real404ArtworkRecovery = { realErrorEvents: 1, remountedSameHash: true, decoded: true };
  } finally { await client.send('Network.setCacheDisabled', { cacheDisabled: false }); await client.detach(); }
}
try {
  await guard(); await library();
  if (mode === 'online') {
    await localUI(); await search('ヨルシカ');
    await page.getByRole('button', { name: '删除搜索记录：ヨルシカ', exact: true }).click(); await until(async () => !(await history()).some(e => e.query === 'ヨルシカ'));
    await search('ヨルシカ'); await page.getByRole('button', { name: '清空搜索记录', exact: true }).click(); await until(async () => (await history()).length === 0);
    await search('情非得已'); report.searchHistoryUI = true;
    // A failed previous attempt may have left a loaded or completed audio cache.
    // Reset only this guarded acceptance fixture before progressive-play checks.
    await pause(); await h.reload(); await call('ClearAudioCache');
    await until(async () => { const s = await audioStats(); return s.usedBytes === 0 && s.activeDownloads === 0; });
    const reset = await savedQueue(); await call('SaveQueue', { ...reset, positionMs: 0, revision: reset.revision + 1 }); await h.reload();
    await instrument(); report.startupWallMs = await play(); report.firstPlayingAudioStats = await audioStats();
    assert.equal(report.firstPlayingAudioStats.cachedTracks, 0); assert.equal(report.firstPlayingAudioStats.activeDownloads, 1);
    await until(async () => (await audioStats()).cachedTracks === 1, 60000); report.completeAudioStats = await audioStats();
    assert.equal(report.completeAudioStats.activeDownloads, 0); assert.ok(report.completeAudioStats.usedBytes > 0);
    await seek(81000, true); report.actualRangeSeekMs = 81000;
    report.resources = await resources();
    await staleArtworkRecovery(); report.resources = await resources(report.resources);
    await nav('设置').click(); const beforeAudio = await audioStats();
    await page.getByLabel('启用封面与歌词缓存', { exact: true }).uncheck(); await until(async () => !(await call('GetSettings')).resourceCache.enabled);
    await page.getByRole('button', { name: '清理封面与歌词缓存', exact: true }).click(); await until(async () => { const s = await resourceStats(); return s.cachedCovers === 0 && s.cachedLyrics === 0; });
    const t = await page.evaluate(() => window.__library04Audio.currentTime); await wait(1100); assert.ok(await page.evaluate(() => !window.__library04Audio.paused && window.__library04Audio.currentTime) > t);
    assert.equal((await audioStats()).cachedTracks, beforeAudio.cachedTracks); assert.equal((await ids()).length, 6010);
    await page.getByLabel('启用封面与歌词缓存', { exact: true }).check(); await until(async () => (await call('GetSettings')).resourceCache.enabled);
    report.resources = await resources(report.resources); report.resourceClearPreservesAudioAndLibrary = true;
    await retainPosition(); report.singleAudioExecutor = await page.evaluate(() => window.__library04Players.size === 1);
  } else {
    const online = JSON.parse(await readFile(`${output}/library04-online-report.json`, 'utf8')); assert.equal(online.pass, true);
    assert.ok((await history()).some(e => e.query === '情非得已'));
    await page.getByRole('button', { name: '播放', exact: true }).waitFor(); report.restoredPaused = true;
    if (mode === 'offline') {
      assert.equal((await savedQueue()).positionMs, 124000); report.resources = await resources(online.resources); assert.ok(report.resources.wallMs < 5000);
      const before = await requestCount(); await instrument(); report.startupWallMs = await play(10000);
      assert.match(await page.locator('footer .stream-source').textContent(), /本地缓存/); await seek(81000, true);
      assert.equal(await requestCount(), before); assert.equal((await audioStats()).activeDownloads, 0); report.audioDiagnosticRequestDelta = 0;
      await retainPosition(); report.singleAudioExecutor = true;
    } else if (mode === 'prepare-crash') {
      await pause(); await h.reload(); await call('ClearAudioCache'); await until(async () => (await audioStats()).usedBytes === 0 && (await audioStats()).activeDownloads === 0);
      const q = await savedQueue(); await call('SaveQueue', { ...q, positionMs: 0, revision: q.revision + 1 }); await h.reload(); await instrument();
      report.startupWallMs = await play(); report.firstPlayingAudioStats = await audioStats();
      assert.equal(report.firstPlayingAudioStats.cachedTracks, 0); assert.equal(report.firstPlayingAudioStats.activeDownloads, 1);
      report.committedQueue = publicQueue(await seek(3000, true)); report.favoriteCount = (await ids()).length; report.searchHistoryCount = (await history()).length;
      report.partialAudioStats = await audioStats(); assert.equal(report.partialAudioStats.cachedTracks, 0); assert.equal(report.partialAudioStats.activeDownloads, 1);
      report.leavePlayingForCallerKill = true;
    } else {
      const expected = JSON.parse(await readFile(`${output}/library04-precrash-state.json`, 'utf8')); assert.equal(expected.pass, true);
      const q = await savedQueue(); assert.ok(q.revision >= expected.committedQueue.revision); assert.ok(q.positionMs >= expected.committedQueue.positionMs);
      assert.equal(q.currentEntryId, expected.committedQueue.currentEntryId); assert.deepEqual(q.entries.map(e => e.queueEntryId), expected.committedQueue.entryIDs);
      assert.equal((await ids()).length, expected.favoriteCount); assert.equal((await history()).length, expected.searchHistoryCount);
      report.recoveredQueue = publicQueue(q); report.resources = await resources(online.resources);
      report.audioStats = await audioStats(); assert.equal(report.audioStats.cachedTracks, 0); assert.equal(report.audioStats.activeDownloads, 0); report.incompleteAudioNotPublished = true;
    }
  }
  report.pass = true;
} catch (error) {
  report.pass = false; report.failure = error instanceof assert.AssertionError ? 'ASSERTION_FAILED' : 'NATIVE_CHECK_FAILED';
  report.failureLocation = error.stack?.match(/native-library04-acceptance\.mjs:\d+:\d+/)?.[0] ?? 'NATIVE_HELPER';
  // Do not serialize exception messages, renderer alerts, network URLs or bodies.
  process.exitCode = 1;
} finally {
  const filename = mode === 'prepare-crash' ? 'library04-precrash-state.json' : `library04-${mode}-report.json`;
  await writeFile(`${output}/${filename}`, JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
  await h.browser.close(); // CDP disconnect only; never close or kill the native host.
}
