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
// orchestrator outage ("El servicio de análisis está offline") apart from a genuinely
// unscannable demo ("No se pudo escanear esa demo"). Both branches are mocked at the
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

    await expect(
      page.getByText('El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'),
    ).toBeVisible();
    // The misleading bad-demo copy must NOT appear for an outage.
    await expect(page.getByText('No se pudo escanear esa demo. Prueba con otro archivo .dem.')).toHaveCount(0);
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

    await expect(page.getByText('No se pudo escanear esa demo. Prueba con otro archivo .dem.')).toBeVisible();
    await expect(
      page.getByText('El servicio de análisis está offline. Arráncalo y vuelve a intentarlo.'),
    ).toHaveCount(0);
  });

  test('reports an empty roster and restores the dropzone instead of stranding the user', async ({ page }) => {
    // A demo whose magic bytes pass the header checks can still scan to zero
    // players (e.g. a Source-1 demo). The job reaches "scanned" and the roster
    // is a valid but empty list - the flow must not advance to an empty picker.
    await page.route('**/api/demos/scan', (route) =>
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ jobId: '00000000-0000-4000-8000-000000000002' }),
      }),
    );
    await page.route('**/api/demos/*/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        // online:true keeps the cloud-mode poller off its PC_OFFLINE branch.
        body: JSON.stringify({ status: 'scanned', online: true }),
      }),
    );
    await page.route('**/api/demos/*/roster', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ players: [] }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(
      page.getByText(
        '¿Seguro que es una demo de CS2? Prueba con otro archivo .dem.',
        { exact: false },
      ),
    ).toBeVisible();
    // The picker heading must never appear for an empty roster...
    await expect(page.getByRole('heading', { name: '¿A QUIÉN QUIERES CLIPEAR?' })).toHaveCount(0);
    // ...and the dropzone is back, proving the stage reset (user not stranded).
    await expect(page.getByText('SUELTA UN .DEM AQUÍ')).toBeVisible();
  });
});

// Roster scoreboard enrichments: match header, multi-kill Highlights chips, and
// the "Recommended" pick. Mocked at the network layer (scan -> status -> roster)
// so it is fast and deterministic, exercising the same fields a real orchestrator
// scan will eventually return.
test.describe('upload roster scoreboard', () => {
  test('shows the match header, highlight chips, and the recommended pick', async ({ page }) => {
    await page.route('**/api/demos/scan', (route) =>
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ jobId: '00000000-0000-4000-8000-000000000001' }),
      }),
    );
    await page.route('**/api/demos/*/status', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'scanned', online: true }),
      }),
    );
    await page.route('**/api/demos/*/roster', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          players: [
            {
              steamid64: '76561198000000001',
              name: 'aceplayer',
              team: 'CT',
              kills: 31,
              deaths: 16,
              assists: 5,
              rating: 1.62,
              rounds_5k: 1,
              rounds_4k: 0,
              rounds_3k: 2,
            },
            {
              steamid64: '76561198000000002',
              name: 'quietplayer',
              team: 'T',
              kills: 12,
              deaths: 20,
              assists: 3,
              rating: 0.7,
            },
          ],
          match: { map: 'de_dust2', score_ct: 9, score_t: 13, rounds: 22 },
        }),
      }),
    );

    await page.goto('/upload');
    await page.locator(FILE_INPUT).setInputFiles(DUMMY_DEM);

    await expect(page.getByRole('heading', { name: '¿A QUIÉN QUIERES CLIPEAR?' })).toBeVisible();

    // Match header: prettified map name and both sides' score.
    await expect(page.getByText('Dust2')).toBeVisible();
    await expect(page.getByText('22 rondas')).toBeVisible();

    // Highlights chips for the ace/3K player, ACE before 3K.
    const aceRow = page.locator('button', { hasText: 'aceplayer' });
    await expect(aceRow.getByText('ACE ×1')).toBeVisible();
    await expect(aceRow.getByText('3K ×2')).toBeVisible();

    // The ace/3K player is the recommended pick, tagged and preselected.
    await expect(aceRow.getByText('Recomendado')).toBeVisible();

    // The quiet player has no multi-kill rounds: a muted placeholder, no chips.
    const quietRow = page.locator('button', { hasText: 'quietplayer' });
    await expect(quietRow.getByText('Recomendado')).toHaveCount(0);
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
    await expect(page.getByRole('heading', { name: '¿A QUIÉN QUIERES CLIPEAR?' })).toBeVisible({ timeout: 150_000 });
    // At least one player row (each carries a crosshair icon) must render.
    await expect(page.locator('button:has(.lucide-crosshair)').first()).toBeVisible();
  });
});
