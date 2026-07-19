// FragForge Studio UI e2e: launches the real Electron app (dev layout, same
// build-resources the installer bundles), waits for the full boot sequence
// (orchestrator + Next server + window navigation), and exercises the shell UI
// through Playwright's Electron driver.
//
// Prerequisites: `pnpm run build` (dist/main.js) and `pnpm run assemble`
// (build-resources/). The app allocates its own loopback ports, and the
// isolated-userdata.cjs bootstrap gives the suite its own userData (and thus
// its own single-instance lock), so it runs even while a real FragForge
// Studio instance is open.
//
// Run: pnpm run test:e2e:ui

import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { after, before, test } from 'node:test';

const require = createRequire(import.meta.url);
const { _electron } = require('playwright-core');

const desktopRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const artifactsDir = join(desktopRoot, 'e2e', 'artifacts');
// Bootstrap that isolates userData so the app's single-instance lock does not
// collide with a running Studio (see isolated-userdata.cjs).
const bootstrapPath = join(desktopRoot, 'e2e', 'isolated-userdata.cjs');

// First boot provisions runtime tools (HLAE unpack, ffmpeg download) into
// userData, which can take minutes; a warm profile boots in seconds. The
// deadline covers the cold case without hanging forever on a real failure.
const BOOT_DEADLINE_MS = 180_000;

/** @type {import('playwright-core').ElectronApplication} */
let app;
/** @type {import('playwright-core').Page} */
let page;
const pageErrors = [];
const consoleErrors = [];

before(async () => {
  mkdirSync(artifactsDir, { recursive: true });
  app = await _electron.launch({
    executablePath: require('electron'),
    args: [bootstrapPath],
    cwd: desktopRoot,
  });
  page = await app.firstWindow();
  page.on('pageerror', (err) => pageErrors.push(String(err)));
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
});

after(async () => {
  if (page && !page.isClosed()) {
    await page.screenshot({ path: join(artifactsDir, 'final-state.png') }).catch(() => {});
  }
  await app?.close().catch(() => {});
});

test('boots to the matches shell, not the error screen', async () => {
  await page.waitForURL(/^http:\/\/127\.0\.0\.1:\d+\/matches/, { timeout: BOOT_DEADLINE_MS });
  const url = page.url();
  assert.match(url, /^http:\/\/127\.0\.0\.1:\d+\/matches/, `landed on ${url}`);
  // The document titles itself with the shared web product name, while the
  // native window must keep the desktop product name (main.ts suppresses
  // page-title-updated).
  assert.equal(await page.title(), 'FragForge');
  const nativeTitle = await app.evaluate(({ BrowserWindow }) => {
    const win = BrowserWindow.getAllWindows()[0];
    return win ? win.getTitle() : null;
  });
  assert.equal(nativeTitle, 'FragForge Studio');
  await page.screenshot({ path: join(artifactsDir, 'matches.png') });
});

test('renders real shell content', async () => {
  // The error screen is a data: URL; reaching here means we are on the web
  // origin. Still assert the page painted something meaningful.
  await page.waitForLoadState('domcontentloaded');
  const text = await page.evaluate(() => document.body.innerText);
  assert.ok(text.trim().length > 0, 'body rendered no text');
  assert.ok(
    !text.includes('no pudo arrancar'),
    'window shows the boot error screen',
  );
});

test('web -> orchestrator proxy answers from inside the app', async () => {
  const status = await page.evaluate(async () => {
    const res = await fetch('/api/demos/jobs');
    return res.status;
  });
  assert.equal(status, 200);
});

test('renderer produced no uncaught exceptions', () => {
  assert.deepEqual(pageErrors, []);
});

test('renderer console has no errors', () => {
  // Report the exact messages on failure so regressions are diagnosable from
  // CI output alone.
  assert.deepEqual(consoleErrors, []);
});
