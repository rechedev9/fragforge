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
import { execFileSync } from 'node:child_process';
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

function hasCodexDescendant(rootPid) {
  const script = `
    $rootProcessId = ${Number(rootPid)}
    $all = @(Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId, Name, CommandLine)
    $ids = [System.Collections.Generic.HashSet[int]]::new()
    [void]$ids.Add($rootProcessId)
    do {
      $changed = $false
      foreach ($process in $all) {
        if ($ids.Contains([int]$process.ParentProcessId) -and $ids.Add([int]$process.ProcessId)) { $changed = $true }
      }
    } while ($changed)
    if ($all | Where-Object { $ids.Contains([int]$_.ProcessId) -and (($_.Name -match '^codex') -or ($_.CommandLine -match 'codex app-server')) }) { 'true' } else { 'false' }
  `;
  return execFileSync('powershell.exe', ['-NoProfile', '-Command', script], { encoding: 'utf8' }).trim() === 'true';
}

async function waitForCodexDescendant(rootPid) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    if (hasCodexDescendant(rootPid)) return true;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  return false;
}

async function waitForNoCodexDescendant(rootPid) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (!hasCodexDescendant(rootPid)) return true;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  return false;
}

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

test('suspends visual work when unfocused and survives native minimization', async () => {
  try {
    await page.evaluate(() => {
      window.dispatchEvent(new Event('blur'));
    });
    await page.waitForFunction(() => document.documentElement.dataset.windowActivity === 'inactive');

    await page.evaluate(() => {
      window.dispatchEvent(new Event('focus'));
    });
    await page.waitForFunction(() => document.documentElement.dataset.windowActivity === 'active');

    await app.evaluate(({ BrowserWindow }) => {
      BrowserWindow.getAllWindows()[0]?.minimize();
    });
    const minimized = await app.evaluate(({ BrowserWindow }) => {
      return BrowserWindow.getAllWindows()[0]?.isMinimized() ?? false;
    });
    assert.equal(minimized, true);
  } finally {
    await app.evaluate(({ BrowserWindow }) => {
      const win = BrowserWindow.getAllWindows()[0];
      win?.restore();
      win?.show();
      win?.focus();
    });
  }
  await page.waitForFunction(() => document.documentElement.dataset.windowActivity === 'active');
});

test('shows the branded agent, OAuth connection surface, and operation promise', async () => {
  assert.equal(hasCodexDescendant(app.process().pid), false, 'Codex started before explicit activation');
  const openAgent = page.getByRole('button', { name: 'Abrir asistente' });
  await openAgent.click();
  const dialog = page.getByRole('dialog', { name: 'Agente de FragForge' });
  await dialog.waitFor({ state: 'visible' });
  assert.equal(await dialog.getByText('Agente en reposo', { exact: true }).isVisible(), true);
  await dialog.getByRole('button', { name: 'Activar agente' }).click();
  await dialog.getByRole('button', { name: 'Activar agente' }).waitFor({ state: 'hidden' });
  assert.equal(await waitForCodexDescendant(app.process().pid), true, 'Codex did not start after explicit activation');
  assert.equal(await dialog.getByText('Agente FragForge', { exact: true }).isVisible(), true);
  assert.equal(await dialog.getByText('Soy tu agente de FragForge', { exact: true }).isVisible(), true);
  assert.equal(await dialog.getByText(/todas las operaciones de Studio/).isVisible(), true);
  assert.equal(await dialog.getByText(/cuenta personal de Codex/i).isVisible(), true);
  assert.equal(await dialog.getByRole('button', { name: /Conectar con Codex|Desconectar/ }).isVisible(), true);
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]?.minimize());
  assert.equal(await waitForNoCodexDescendant(app.process().pid), true, 'Codex stayed alive after background hibernation');
  await app.evaluate(({ BrowserWindow }) => {
    const win = BrowserWindow.getAllWindows()[0];
    win?.restore();
    win?.show();
    win?.focus();
  });
  await dialog.getByRole('button', { name: 'Activar agente' }).waitFor({ state: 'visible' });
  assert.equal(await dialog.getByText('Soy tu agente de FragForge', { exact: true }).isVisible(), true);
  await dialog.getByRole('button', { name: 'Cerrar' }).click();
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
