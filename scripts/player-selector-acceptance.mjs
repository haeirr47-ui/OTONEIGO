// Render counters are injected by rewriting exact source substrings, so this tool
// is coupled to the implementation text: a formatting or signature change trips
// the "exactly once" assertion instead of silently counting nothing. The counters
// exist only in this dev server; the production build never contains them.
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from '../frontend/node_modules/vite/dist/node/index.js';
import { chromium } from '../.tools/browser/node_modules/playwright/index.mjs';

function resolveBrowser() {
  if (process.env.PLAYER_SELECTOR_BROWSER) return process.env.PLAYER_SELECTOR_BROWSER;
  // This host has Chrome installed rather than a Playwright browser download. When
  // the default path is absent, return undefined so Playwright resolves its own.
  const installed = 'C:/Program Files/Google/Chrome/Application/chrome.exe';
  return existsSync(installed) ? installed : undefined;
}

function replaceExactlyOnce(source, marker, replacement, id) {
  const matches = source.split(marker).length - 1;
  assert.equal(matches, 1, `Expected one render instrumentation marker in ${id}, found ${matches}: ${marker}`);
  return source.replace(marker, replacement);
}

const server = await createServer({
  root: fileURLToPath(new URL('../frontend/', import.meta.url)),
  configFile: fileURLToPath(new URL('../frontend/vite.config.ts', import.meta.url)),
  server: { host: '127.0.0.1', port: 0 },
  appType: 'mpa',
  plugins: [{
    name: 'selector-acceptance-render-counters',
    enforce: 'pre',
    transform(source, id) {
      if (id.endsWith('/src/App.tsx')) return replaceExactlyOnce(source, 'export function App({ bridge }: { bridge: DesktopBridge }) {', 'export function App({ bridge }: { bridge: DesktopBridge }) { globalThis.__selectorProductRenders.App++;', id);
      if (id.endsWith('/src/OtoneiPresentation.tsx')) {
        source = replaceExactlyOnce(source, 'export function OtoneiPlayer({ current: providedCurrent }: { current?: TrackSnapshot } = {}) {', 'export function OtoneiPlayer({ current: providedCurrent }: { current?: TrackSnapshot } = {}) { globalThis.__selectorProductRenders.OtoneiPlayer++;', id);
        source = replaceExactlyOnce(source, '  const active = usePlayerSelector(', '  globalThis.__selectorProductRenders.TrackCard++;\n  const active = usePlayerSelector(', id);
        return replaceExactlyOnce(source, 'function Cover({ track, onClick, large = false }: { track?: TrackSnapshot; onClick?: () => void; large?: boolean }) {', 'function Cover({ track, onClick, large = false }: { track?: TrackSnapshot; onClick?: () => void; large?: boolean }) { globalThis.__selectorProductRenders.Cover++;', id);
      }
      if (id.endsWith('/src/LibraryPanels.tsx')) return replaceExactlyOnce(source, 'export function FavoriteButton({ track }: { track: TrackSnapshot }) {', 'export function FavoriteButton({ track }: { track: TrackSnapshot }) { globalThis.__selectorProductRenders.FavoriteButton++;', id);
    },
  }],
});
let browser;
try {
  await server.listen();
  const address = server.httpServer.address();
  assert.ok(address && typeof address === 'object');
  browser = await chromium.launch({ headless: true, executablePath: resolveBrowser() });
  const page = await browser.newPage();
  page.on('pageerror', error => console.error(error));
  await page.goto(`http://127.0.0.1:${address.port}/player-selector-acceptance.html`);
  await page.evaluate(() => window.__playerSelectorAcceptance.run());
  await page.waitForFunction(() => ['passed', 'failed'].includes(document.body.dataset.result ?? ''), { timeout: 10000 });
  const result = await page.evaluate(() => ({ result: document.body.dataset.result, error: document.body.dataset.error, report: document.body.dataset.report, renders: window.__selectorProductRenders }));
  if (result.result !== 'passed') throw new Error(result.error ?? 'Player selector acceptance failed');
  const report = { browser: 'Chrome headless', evidence: 'Browser DOM and React render instrumentation; not native WebView2', assertions: JSON.parse(result.report), componentRenderCounts: result.renders };
  const reportPath = resolve('output/playwright/player-selector-report.json');
  await mkdir(resolve('output/playwright'), { recursive: true });
  await writeFile(reportPath, `${JSON.stringify(report, null, 2)}\n`, 'utf8');
  console.log(report);
  console.log(`Report: ${reportPath}`);
  console.log('Mounted React player selector acceptance passed. This is Chromium browser evidence, not native WebView2 acceptance.');
} finally {
  await browser?.close();
  await server.close();
}
