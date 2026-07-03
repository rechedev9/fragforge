import { test, expect, type Page, type Route } from '@playwright/test';

/**
 * Covers the highlights list on the match-detail screen ("We found N
 * highlights"): it renders as a vertical list (not a horizontal filmstrip),
 * rows are multi-selectable, Create reel requires at least one pick, and the
 * generate/record request carries every selected segment id in PLAN order
 * (the order rows appear in the list), not click order — 2+ ids render as one
 * concatenated reel.
 *
 * Entirely mocked at the network layer (scan -> roster -> parse -> plan ->
 * record -> render), matching the pattern in create-reel-bookends.spec.ts, so
 * it is fast, deterministic, and needs no orchestrator.
 */

const DUMMY_DEM = {
  name: 'sample.dem',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('HL2DEMO\0'),
};

const JOB_ID = '00000000-0000-4000-8000-0000000000bb';
const STEAM_ID = '76561198000000002';

const ROSTER_BODY = {
  players: [
    { steamid64: STEAM_ID, name: 'aceplayer', team: 'CT', kills: 24, deaths: 11, assists: 5, rating: 1.4 },
  ],
  match: { map: 'de_mirage', score_ct: 13, score_t: 9, rounds: 22 },
};

// Three segments across rounds 1, 6, 9 so plan order (list order) differs from
// an out-of-order click sequence, and Rounds summarize distinctly.
const PLAN_BODY = {
  schema_version: '1',
  demo: { map: 'de_mirage' },
  target: { steamid64: STEAM_ID, name_in_demo: 'aceplayer', team_at_start: 'CT' },
  stats: { total_kills_target: 24 },
  segments: [
    { id: 'seg-001', round: 1, kills: [{ weapon: 'ak47' }] },
    { id: 'seg-006', round: 6, kills: [{ weapon: 'awp' }, { weapon: 'awp' }, { weapon: 'awp' }] },
    { id: 'seg-009', round: 9, kills: [{ weapon: 'deagle' }, { weapon: 'deagle' }] },
  ],
};

/** Wires scan -> roster -> parse -> plan mocks and lands on the match-detail highlights list. */
async function gotoHighlights(page: Page, jobStatus: { value: string }): Promise<void> {
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
  await page.getByRole('heading', { name: 'Who do you want to clip?' }).waitFor();
  await page.locator('button:has(.lucide-crosshair)').first().click();

  await page.waitForURL(/\/matches\//);
  await page.getByText('JUGADAS DETECTADAS · 3').waitFor();
}

function row(page: Page, round: number) {
  return page.getByRole('button', { name: `RONDA ${round}` });
}

/** The sticky create-reel bar's "Selected ..." summary text (distinct from the list header's "N selected" count). */
function selectionSummary(page: Page) {
  return page.locator('.sticky.bottom-0');
}

/** Wires record/render mocks and captures the record POST body's segment_ids. */
function captureRecordSegmentIds(page: Page, jobStatus: { value: string }): { segmentIds: string[] | null } {
  const captured: { segmentIds: string[] | null } = { segmentIds: null };
  page.route('**/api/demos/*/record', (route) => {
    const body = JSON.parse(route.request().postData() ?? '{}') as { segment_ids?: string[] };
    captured.segmentIds = body.segment_ids ?? [];
    jobStatus.value = 'recorded';
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
  });
  page.route('**/api/demos/*/renders/*', async (route: Route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
    }
    return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
  });
  return captured;
}

test.describe('highlights list — vertical, multi-select', () => {
  test('renders highlights as a vertical list, not a horizontal carousel', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoHighlights(page, jobStatus);

    // All three rows are present and visible without any click.
    await expect(row(page, 1)).toBeVisible();
    await expect(row(page, 6)).toBeVisible();
    await expect(row(page, 9)).toBeVisible();

    // A vertical list stacks rows top-to-bottom at (roughly) the same x, and
    // the old horizontal Filmstrip's ScrollArea/scrollbar is gone.
    const box1 = await row(page, 1).boundingBox();
    const box2 = await row(page, 6).boundingBox();
    const box3 = await row(page, 9).boundingBox();
    expect(box1).not.toBeNull();
    expect(box2).not.toBeNull();
    expect(box3).not.toBeNull();
    expect(box2!.y).toBeGreaterThan(box1!.y);
    expect(box3!.y).toBeGreaterThan(box2!.y);
    expect(Math.abs(box2!.x - box1!.x)).toBeLessThan(2);
    expect(Math.abs(box3!.x - box1!.x)).toBeLessThan(2);
    await expect(page.locator('[data-orientation="horizontal"]')).toHaveCount(0);

    // Full row width fits without any horizontal page scroll, even narrow.
    await page.setViewportSize({ width: 380, height: 900 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
  });

  test('Create reel is disabled until at least one highlight is picked', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoHighlights(page, jobStatus);

    const createButton = page.getByRole('button', { name: 'FORJAR REEL' });
    await expect(createButton).toBeDisabled();

    await row(page, 1).click();
    await expect(createButton).toBeEnabled();

    // Toggling the only selection back off disables it again.
    await row(page, 1).click();
    await expect(createButton).toBeDisabled();
  });

  test('a single selection still sends exactly one segment id (unchanged behavior)', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoHighlights(page, jobStatus);
    const captured = captureRecordSegmentIds(page, jobStatus);

    await row(page, 6).click();
    await expect(selectionSummary(page)).toContainText('3K · Ronda 6');

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => captured.segmentIds !== null, { timeout: 30_000 }).toBe(true);
    expect(captured.segmentIds).toEqual(['seg-006']);
  });

  test('multi-select sends every picked segment id in plan order, not click order', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoHighlights(page, jobStatus);
    const captured = captureRecordSegmentIds(page, jobStatus);

    // Click out of plan order: round 9, then round 1, then round 6.
    await row(page, 9).click();
    await row(page, 1).click();
    await row(page, 6).click();

    await expect(selectionSummary(page)).toContainText('3 jugadas · Rondas 1, 6, 9');

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    await expect.poll(() => captured.segmentIds !== null, { timeout: 30_000 }).toBe(true);
    // Plan order (list order), not the 9 -> 1 -> 6 click order.
    expect(captured.segmentIds).toEqual(['seg-001', 'seg-006', 'seg-009']);
  });

  test('Select all picks every highlight and Clear removes the selection', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoHighlights(page, jobStatus);

    await page.getByRole('button', { name: 'SELECCIONAR TODO' }).click();
    await expect(page.getByText('3 SELECCIONADAS')).toBeVisible();
    await expect(page.getByRole('button', { name: 'FORJAR REEL' })).toBeEnabled();

    await page.getByRole('button', { name: 'LIMPIAR' }).click();
    await expect(page.getByText('TOCA PARA SELECCIONAR')).toBeVisible();
    await expect(page.getByRole('button', { name: 'FORJAR REEL' })).toBeDisabled();
  });
});
