import { test, expect } from '@playwright/test';

/**
 * FragForge cloud data plane (local-first): the hosted web is control plane only.
 * After pairing, the browser reads the agent's loopback endpoint from
 * /api/pc/status, then talks straight to http://127.0.0.1:<port> with a Bearer
 * token for the whole pipeline — upload (scan), status poll, roster. These specs
 * mock the control-plane /api/pc/status and the loopback origin at the network
 * layer (no orchestrator, no agent, no Supabase), so they exercise exactly the
 * client's loopback transport and offline handling in RealApiClient.
 */

const DUMMY_DEM = {
  name: 'cloud.dem',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('HL2DEMO\0'),
};

const FILE_INPUT = 'input[type=file]';
const JOB_ID = '11111111-1111-4111-8111-111111111111';
const LOOPBACK = 'http://127.0.0.1:8090';

/** Control plane: report a paired, online agent whose loopback the browser dials. */
async function mockPcStatus(page: import('@playwright/test').Page, online = true): Promise<void> {
  await page.route('**/api/pc/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ paired: true, online, loopback: { port: 8090, token: 'tok-e2e' } }),
    }),
  );
}

/** Loopback: agent is alive (GET /healthz ok). */
async function mockHealthzOk(page: import('@playwright/test').Page): Promise<void> {
  await page.route(`${LOOPBACK}/healthz`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) }),
  );
}

/** Loopback: accept the multipart .dem upload (POST /api/jobs) and hand back a job id. */
async function mockScan(page: import('@playwright/test').Page): Promise<void> {
  await page.route(`${LOOPBACK}/api/jobs`, (route) =>
    route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ id: JOB_ID }),
    }),
  );
}

test.describe('cloud upload — direct-to-loopback data plane', () => {
  test('probes healthz, uploads, polls status, then shows the roster', async ({ page }) => {
    await mockPcStatus(page);
    await mockHealthzOk(page);
    await mockScan(page);

    let statusCalls = 0;
    // GET /api/jobs/{id} — the orchestrator's native status (no /status suffix).
    await page.route(`${LOOPBACK}/api/jobs/${JOB_ID}`, (route) => {
      statusCalls++;
      const status = statusCalls <= 2 ? 'scanning' : 'scanned';
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status }) });
    });
    await page.route(`${LOOPBACK}/api/jobs/${JOB_ID}/roster`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          players: [
            { steamid64: '76561198000000001', name: 'zack', team: 'T', kills: 20, deaths: 10, assists: 4 },
            { steamid64: '76561198000000002', name: 'video', team: 'CT', kills: 15, deaths: 12, assists: 6 },
          ],
        }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByRole('heading', { name: '¿A QUIÉN QUIERES CLIPEAR?' })).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText('zack')).toBeVisible();
    await expect(page.getByText('video')).toBeVisible();
  });

  test('shows a PC-offline state when the loopback healthz probe fails', async ({ page }) => {
    await mockPcStatus(page, false);
    // Loopback down: the healthz probe cannot connect.
    await page.route(`${LOOPBACK}/healthz`, (route) => route.abort());

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByText('Tu PC está offline')).toBeVisible({ timeout: 15_000 });
    await expect(
      page.getByText('Abre FragForge Agent en tu PC para analizar esta demo y reintenta.'),
    ).toBeVisible();
    // The offline card must offer the no-login pairing path, not a dead end.
    await expect(page.getByRole('link', { name: 'Empareja este PC' })).toHaveAttribute(
      'href',
      '/connect?step=pair',
    );
  });

  test('lands on the PC-offline state when the loopback dies mid-upload', async ({ page }) => {
    // Healthz passes (the PC was up at probe time), but the loopback drops before
    // the scan POST lands — the classic "PC slept mid-flight". The client must
    // translate that fetch rejection to PC_OFFLINE and show the actionable offline
    // state, not a raw network error / generic scan failure.
    await mockPcStatus(page);
    await mockHealthzOk(page);
    await page.route(`${LOOPBACK}/api/jobs`, (route) => route.abort());

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByText('Tu PC está offline')).toBeVisible({ timeout: 15_000 });
    await expect(
      page.getByText('Abre FragForge Agent en tu PC para analizar esta demo y reintenta.'),
    ).toBeVisible();
  });

  test('reports a scan failure and returns to the dropzone', async ({ page }) => {
    await mockPcStatus(page);
    await mockHealthzOk(page);
    await mockScan(page);

    await page.route(`${LOOPBACK}/api/jobs/${JOB_ID}`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'failed', failure_reason: 'boom' }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByText('No se pudo escanear esa demo. Prueba con otro archivo .dem.')).toBeVisible({ timeout: 15_000 });
  });
});
