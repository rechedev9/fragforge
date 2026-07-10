import { test, expect, type Page, type Route } from '@playwright/test';

/**
 * Covers the intro/outro bookend text fields on the create-reel screen
 * (match detail page, "Bookends" chips under "Edit options"): toggling a
 * chip reveals a text input, typed text flows into the render-variant POST
 * body as intro_text/outro_text, an empty input omits the field, and the
 * client enforces the 80-char limit before a request is ever sent.
 *
 * Entirely mocked at the network layer (scan -> roster -> parse -> plan ->
 * record -> render), matching the pattern in upload.spec.ts, so it is fast,
 * deterministic, and needs no orchestrator.
 */

const DUMMY_DEM = {
  name: 'sample.dem',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('HL2DEMO\0'),
};

const JOB_ID = '00000000-0000-4000-8000-0000000000aa';
const STEAM_ID = '76561198000000001';

const ROSTER_BODY = {
  players: [
    { steamid64: STEAM_ID, name: 'aceplayer', team: 'CT', kills: 20, deaths: 10, assists: 4, rating: 1.3 },
  ],
  match: { map: 'de_mirage', score_ct: 13, score_t: 9, rounds: 22 },
};

const PLAN_BODY = {
  schema_version: '1',
  demo: { map: 'de_mirage' },
  target: { steamid64: STEAM_ID, name_in_demo: 'aceplayer', team_at_start: 'CT' },
  stats: { total_kills_target: 20 },
  segments: [{ id: 'seg-001', round: 3, kills: [{ weapon: 'ak47' }] }],
};

/**
 * Wires the scan -> picker -> match-detail mocks and drives the UI there, with
 * one play pre-selected. `jobStatus` is a mutable box driving /status: it
 * starts at 'scanned' (so the cloud scan poll resolves to the roster picker),
 * flips to 'parsed' once /parse is POSTed (so parseDemo's own status poll
 * resolves), and the caller flips it to 'recorded' once the record POST fires
 * so the reconcile loop reaches the render POST deterministically.
 */
async function gotoCreateReel(page: Page, jobStatus: { value: string }): Promise<void> {
  jobStatus.value = 'scanned';
  await page.route('**/api/demos/scan', (route) =>
    route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ jobId: JOB_ID }) }),
  );
  await page.route('**/api/demos/*/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: jobStatus.value, online: true }),
    }),
  );
  await page.route('**/api/demos/*/roster', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(ROSTER_BODY) }),
  );
  await page.route('**/api/demos/*/parse', (route) => {
    jobStatus.value = 'parsed';
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
  });
  await page.route('**/api/demos/*/plan', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(PLAN_BODY) }),
  );

  await page.goto('/upload');
  await page.locator('input[type=file]').setInputFiles(DUMMY_DEM);
  await page.getByRole('heading', { name: '¿A QUIÉN QUIERES CLIPEAR?' }).waitFor();
  await page.getByTestId('player-avatar').first().click();

  await page.waitForURL(/\/matches\//);
  await page.getByText('JUGADAS DETECTADAS · 1').waitFor();

  // Pick the one play so "FORJAR REEL" is enabled.
  await page.locator('button:has(.lucide-crosshair)').first().click();
}

/** Intro (Apertura) chip toggle in the Bookends row. */
function introChip(page: Page) {
  return page.getByRole('button', { name: 'Apertura' });
}
function outroChip(page: Page) {
  return page.getByRole('button', { name: 'Cierre' });
}
function introInput(page: Page) {
  return page.getByLabel('Título de apertura');
}
function outroInput(page: Page) {
  return page.getByLabel('Texto de cierre');
}
function hookTextToggle(page: Page) {
  return page.getByRole('button', { name: 'Título automático' });
}
function killCounterToggle(page: Page) {
  return page.getByRole('button', { name: 'Contador de kills' });
}

test.describe('create-reel bookend text', () => {
  test('automatic text controls default off and serialize false', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    await expect(hookTextToggle(page)).toHaveAttribute('aria-pressed', 'false');
    await expect(killCounterToggle(page)).toHaveAttribute('aria-pressed', 'false');

    const captured: {
      recordBody?: { edit?: Record<string, unknown> };
      renderBody?: { edit?: Record<string, unknown> };
    } = {};
    await page.route('**/api/demos/*/record', (route) => {
      captured.recordBody = JSON.parse(route.request().postData() ?? '{}');
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        captured.renderBody = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => captured.recordBody !== undefined, { timeout: 30_000 }).toBe(true);
    await expect.poll(() => captured.renderBody !== undefined, { timeout: 30_000 }).toBe(true);
    expect(captured.recordBody?.edit?.hook_text).toBe(false);
    expect(captured.recordBody?.edit?.kill_counter).toBe(false);
    expect(captured.renderBody?.edit?.hook_text).toBe(false);
    expect(captured.renderBody?.edit?.kill_counter).toBe(false);
  });

  test('automatic hook and kill counter toggle independently and serialize true', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    await hookTextToggle(page).click();
    await expect(hookTextToggle(page)).toHaveAttribute('aria-pressed', 'true');
    await expect(killCounterToggle(page)).toHaveAttribute('aria-pressed', 'false');

    await killCounterToggle(page).click();
    await expect(hookTextToggle(page)).toHaveAttribute('aria-pressed', 'true');
    await expect(killCounterToggle(page)).toHaveAttribute('aria-pressed', 'true');

    const captured: {
      recordBody?: { edit?: Record<string, unknown> };
      renderBody?: { edit?: Record<string, unknown> };
    } = {};
    await page.route('**/api/demos/*/record', (route) => {
      captured.recordBody = JSON.parse(route.request().postData() ?? '{}');
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        captured.renderBody = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => captured.recordBody !== undefined, { timeout: 30_000 }).toBe(true);
    await expect.poll(() => captured.renderBody !== undefined, { timeout: 30_000 }).toBe(true);
    expect(captured.recordBody?.edit?.hook_text).toBe(true);
    expect(captured.recordBody?.edit?.kill_counter).toBe(true);
    expect(captured.renderBody?.edit?.hook_text).toBe(true);
    expect(captured.renderBody?.edit?.kill_counter).toBe(true);
  });

  test('landscape format is sent with the record request', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    const captured: { recordBody?: { edit?: { format?: string } } } = {};
    await page.route('**/api/demos/*/record', (route) => {
      captured.recordBody = JSON.parse(route.request().postData() ?? '{}');
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    await page.getByRole('button', { name: '16:9' }).click();
    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => captured.recordBody !== undefined, { timeout: 30_000 }).toBe(true);
    const recordBody = captured.recordBody;
    if (recordBody === undefined) throw new Error('record request was not captured');
    expect(recordBody.edit?.format).toBe('landscape-16x9');
  });

  test('toggling Intro reveals its text input, toggling off hides it again', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    await expect(introInput(page)).toBeHidden();
    await introChip(page).click();
    await expect(introInput(page)).toBeVisible();
    await expect(introInput(page)).toHaveAttribute(
      'placeholder',
      'Título de apertura (vacío = titular generado)',
    );

    await introChip(page).click();
    await expect(introInput(page)).toBeHidden();
  });

  test('typed intro/outro text flows into the render request body', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    let renderBody: { edit?: Record<string, unknown> } | null = null;
    await page.route('**/api/demos/*/record', (route) => {
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        renderBody = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      // GET: render not started until the POST above lands.
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    await introChip(page).click();
    await introInput(page).fill('GG WP everyone');
    await outroChip(page).click();
    await outroInput(page).fill('@fragforge');

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => renderBody !== null, { timeout: 30_000 }).toBe(true);
    const edit = renderBody!.edit as Record<string, unknown>;
    expect(edit.intro).toBe(true);
    expect(edit.outro).toBe(true);
    expect(edit.intro_text).toBe('GG WP everyone');
    expect(edit.outro_text).toBe('@fragforge');
  });

  test('an empty bookend input omits its text field from the request', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    let renderBody: { edit?: Record<string, unknown> } | null = null;
    await page.route('**/api/demos/*/record', (route) => {
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        renderBody = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    // Intro toggled on but left empty; Outro left off entirely.
    await introChip(page).click();

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => renderBody !== null, { timeout: 30_000 }).toBe(true);
    const edit = renderBody!.edit as Record<string, unknown>;
    expect(edit.intro).toBe(true);
    expect(edit.outro).toBe(false);
    expect(edit).not.toHaveProperty('intro_text');
    expect(edit).not.toHaveProperty('outro_text');
  });

  test('the intro input rejects more than 80 characters client-side', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    await introChip(page).click();
    await expect(introInput(page)).toHaveAttribute('maxlength', '80');

    const tooLong = 'x'.repeat(120);
    await introInput(page).fill(tooLong);
    await expect(introInput(page)).toHaveValue('x'.repeat(80));

    let renderBody: { edit?: Record<string, unknown> } | null = null;
    await page.route('**/api/demos/*/record', (route) => {
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        renderBody = JSON.parse(route.request().postData() ?? '{}');
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => renderBody !== null, { timeout: 30_000 }).toBe(true);
    const edit = renderBody!.edit as Record<string, unknown>;
    expect((edit.intro_text as string).length).toBe(80);
  });
});
