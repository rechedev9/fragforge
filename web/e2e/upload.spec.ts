import { test, expect } from '@playwright/test';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

/**
 * A minimal valid-looking .dem payload, set straight on the file input. The
 * dropzone only validates the `.dem` extension client-side, so this is enough to
 * reach the scan call without shipping a real demo into the repo (real .dem
 * files are never committed). The error-messaging specs below mock the network,
 * so the bytes are never actually parsed.
 */
const DUMMY_DEM = {
  name: 'sample.dem',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('HL2DEMO\0'),
};

const FILE_INPUT = 'input[type=file]';

// Regression specs for the offline-detection fix: the /upload flow must tell an
// orchestrator outage ("Analysis service is offline") apart from a genuinely
// unscannable demo ("Could not scan that demo"). Both branches are mocked at the
// network layer, so they are fast, deterministic, and need no orchestrator.
test.describe('upload error messaging', () => {
  test('reports the analysis service as offline when the scan proxy returns 503 + code', async ({ page }) => {
    await page.route('**/api/demos/scan', (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'analysis service unavailable', code: 'service_unavailable' }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByText('Analysis service is offline. Start it and try again.')).toBeVisible();
    // The misleading bad-demo copy must NOT appear for an outage.
    await expect(page.getByText('Could not scan that demo. Try another .dem file.')).toHaveCount(0);
  });

  test('reports a bad demo when the scan fails for a reason other than an outage', async ({ page }) => {
    // The scan is accepted (job created) but the roster scan then fails - the
    // classic unparseable-demo path, which must not be mistaken for an outage.
    await page.route('**/api/demos/scan', (route) =>
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ jobId: '00000000-0000-4000-8000-000000000000' }),
      }),
    );
    await page.route('**/api/demos/*/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'failed' }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByText('Could not scan that demo. Try another .dem file.')).toBeVisible();
    await expect(page.getByText('Analysis service is offline. Start it and try again.')).toHaveCount(0);
  });
});

// Full happy path against the real pipeline. Gated on a reachable orchestrator
// and a real demo fixture (ZV_E2E_DEMO, default ../testdata/sample.dem) so it
// skips - never fails - when those are absent.
const DEMO_PATH = process.env.ZV_E2E_DEMO ?? resolve(__dirname, '../../testdata/sample.dem');
const ORCHESTRATOR = process.env.ZV_E2E_ORCHESTRATOR ?? 'http://127.0.0.1:8080';

test.describe('upload happy path (real demo + orchestrator)', () => {
  test('scans a real demo and lists its roster', async ({ page, request }) => {
    test.skip(!existsSync(DEMO_PATH), `no demo fixture at ${DEMO_PATH} (set ZV_E2E_DEMO)`);
    const orchestratorUp = await request
      .get(`${ORCHESTRATOR}/healthz`)
      .then((r) => r.ok())
      .catch(() => false);
    test.skip(!orchestratorUp, `orchestrator not reachable at ${ORCHESTRATOR}`);

    // A ~400 MB upload plus a roster parse - generous budget, never the default 30s.
    test.setTimeout(180_000);

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DEMO_PATH);

    // Scanning... -> the picker. The heading only switches once a roster exists.
    await expect(page.getByRole('heading', { name: 'Who do you want to clip?' })).toBeVisible({ timeout: 150_000 });
    // At least one player row (each carries a crosshair icon) must render.
    await expect(page.locator('button:has(.lucide-crosshair)').first()).toBeVisible();
  });
});
