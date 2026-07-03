import { test, expect } from '@playwright/test';

/**
 * FragForge Cloud async scan flow (Task 13): the browser uploads a .dem, then
 * polls /api/demos/{jobId}/status until the paired agent reports `scanned`,
 * then fetches the roster. These specs mock every /api/demos/* call at the
 * network layer (no orchestrator, no Supabase), so they are fast, deterministic,
 * and exercise exactly the client polling/offline logic in RealApiClient.
 * waitForScan and the /upload page's `waiting-for-pc` stage.
 */

const DUMMY_DEM = {
  name: 'cloud.dem',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('HL2DEMO\0'),
};

const FILE_INPUT = 'input[type=file]';
const JOB_ID = '11111111-1111-4111-8111-111111111111';

async function mockScan(page: import('@playwright/test').Page): Promise<void> {
  await page.route('**/api/demos/scan', (route) =>
    route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ jobId: JOB_ID }),
    }),
  );
}

test.describe('cloud upload — async scan', () => {
  test('polls status until scanned, then shows the roster', async ({ page }) => {
    await mockScan(page);

    let statusCalls = 0;
    await page.route('**/api/demos/*/status', (route) => {
      statusCalls++;
      const status = statusCalls <= 2 ? 'scanning' : 'scanned';
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status, online: true }),
      });
    });
    await page.route('**/api/demos/*/roster', (route) =>
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

  test('shows a PC-offline state when the agent never heartbeats', async ({ page }) => {
    test.setTimeout(30_000);
    await mockScan(page);

    await page.route('**/api/demos/*/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'scanning', online: false }),
      }),
    );
    // The client throws before ever fetching the roster in this scenario, but
    // route it anyway so a bug that reaches this call fails loudly instead of
    // hanging on an unmocked request.
    await page.route('**/api/demos/*/roster', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ players: [] }) }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    // waitForScan throws PC_OFFLINE after >4 consecutive offline polls at
    // 1500ms each (~7.5s), well inside the 30s test timeout above.
    await expect(page.getByText('Tu PC está offline')).toBeVisible({ timeout: 20_000 });
    await expect(
      page.getByText('Abre FragForge Agent en tu PC para analizar esta demo y reintenta.'),
    ).toBeVisible();
  });

  test('reports a scan failure and returns to the dropzone', async ({ page }) => {
    await mockScan(page);

    await page.route('**/api/demos/*/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'failed', failure_reason: 'boom', online: true }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByText('No se pudo escanear esa demo. Prueba con otro archivo .dem.')).toBeVisible({ timeout: 15_000 });
  });
});
