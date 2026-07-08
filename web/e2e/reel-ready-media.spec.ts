import { test, expect, type Page, type Route } from '@playwright/test';

/**
 * Regression for the "LISTO reel, but MP4/cover 404" bug: the client used to
 * build the reel's artifact file name client-side as the segment ids joined
 * (e.g. "seg-001"), but the editor writes a single "demo-compilation.mp4"/.jpg.
 * The download and cover thumbnail therefore 404'd. The fix is server-reported
 * names: the render-variant GET now carries `videos`/`covers`, and the ready
 * reel must address its cover (and video) by that server name.
 *
 * Entirely mocked at the network layer (scan -> roster -> parse -> plan ->
 * record -> render -> ready), matching create-reel-bookends.spec.ts, so it is
 * fast, deterministic, and needs no orchestrator. Runs in local mode (the dev
 * server's default), so the reel media is a same-origin /api/demos URL the
 * <img>/<video> load directly - exactly the path that 404'd.
 */

const DUMMY_DEM = {
  name: 'sample.dem',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('HL2DEMO\0'),
};

const JOB_ID = '00000000-0000-4000-8000-0000000000cc';
const STEAM_ID = '76561198000000001';
const VARIANT = 'viral-60-clean';
// The editor writes one all-kills compilation short under this name, not the
// segment id the client used to guess.
const ARTIFACT_NAME = 'demo-compilation';
// A 1x1 JPEG so the <img> actually decodes when the browser loads the cover.
const JPEG_1PX = Buffer.from(
  '/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA=',
  'base64',
);

const ROSTER_BODY = {
  players: [{ steamid64: STEAM_ID, name: 'aceplayer', team: 'CT', kills: 20, deaths: 10, assists: 4, rating: 1.3 }],
  match: { map: 'de_mirage', score_ct: 13, score_t: 9, rounds: 22 },
};

const PLAN_BODY = {
  schema_version: '1',
  demo: { map: 'de_mirage' },
  target: { steamid64: STEAM_ID, name_in_demo: 'aceplayer', team_at_start: 'CT' },
  stats: { total_kills_target: 20 },
  segments: [{ id: 'seg-001', round: 3, kills: [{ weapon: 'ak47' }] }],
};

/** Wires scan -> picker -> match-detail and lands on the highlights list with one play pre-picked. */
async function gotoCreateReel(page: Page, jobStatus: { value: string }): Promise<void> {
  jobStatus.value = 'scanned';
  await page.route('**/api/demos/scan', (route) =>
    route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ jobId: JOB_ID }) }),
  );
  await page.route('**/api/demos/*/status', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: jobStatus.value, online: true }) }),
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
  await page.locator('button:has(.lucide-crosshair)').first().click();
}

test.describe('ready reel media addressing', () => {
  test('the ready reel loads its cover by the server-reported name, not the segment id', async ({ page }) => {
    const jobStatus = { value: 'parsed' };
    await gotoCreateReel(page, jobStatus);

    // record -> recorded so the reconcile loop drives the render POST.
    await page.route('**/api/demos/*/record', (route) => {
      jobStatus.value = 'recorded';
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    });

    // The render-variant endpoint: 404 until the POST lands, then ready with the
    // editor's real artifact names (a single "demo-compilation" compilation).
    // The `*` in this glob does not span '/', so it does not shadow the more
    // specific videos/covers sub-routes below.
    let renderPosted = false;
    await page.route('**/api/demos/*/renders/*', async (route: Route) => {
      if (route.request().method() === 'POST') {
        renderPosted = true;
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'queued' }) });
      }
      if (renderPosted) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'ready', videos: [ARTIFACT_NAME], covers: [ARTIFACT_NAME] }),
        });
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) });
    });

    // Serve the cover only at the server-reported name and record which name the
    // <img> actually requested. A request for the old segment-joined name would
    // never hit this route (and would 404 in the real product).
    let coverRequestPath = '';
    await page.route(`**/api/demos/*/renders/*/covers/*`, (route) => {
      coverRequestPath = new URL(route.request().url()).pathname;
      return route.fulfill({ status: 200, contentType: 'image/jpeg', body: JPEG_1PX });
    });

    await page.getByRole('button', { name: 'FORJAR REEL' }).click();
    await page.waitForURL(/\/videos/);

    // The ready card renders <img src={thumbnailUrl}>; that src must be the
    // server-reported cover name.
    const cover = page.locator('[data-slot="card"] img').first();
    await expect(cover).toBeVisible({ timeout: 30_000 });
    await expect(cover).toHaveAttribute('src', new RegExp(`/renders/${VARIANT}/covers/${ARTIFACT_NAME}$`));

    // And the browser actually fetched that name (never the segment-joined guess).
    await expect.poll(() => coverRequestPath, { timeout: 30_000 }).toContain(`/covers/${ARTIFACT_NAME}`);
    expect(coverRequestPath).not.toContain('/covers/seg-001');
  });
});
