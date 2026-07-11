import { test, expect } from '@playwright/test';
import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

/**
 * Live visibility on /matches (milestone 5). The matches page polls
 * GET /api/demos (its only mount-time network call - it never hits /status or
 * /plan unless a card is clicked), so a job created out of band (via the MCP
 * server or plain HTTP) must surface here with no UI action and no reload.
 *
 * The primary spec mocks /api/demos at the network layer, so it is fast,
 * deterministic, and needs neither an orchestrator nor a real .dem: it proves
 * the poll loop + listMatches + proxy-shape mapping, which is the whole feature.
 */

const ARTIFACT_DIR = resolve(__dirname, '../e2e-artifacts');

// A single out-of-band job, already parsed (kill plan present), as the list
// proxy projects it: flat, snake_case JobSummary. `map: de_inferno` prettifies
// to "Inferno" on the card; target_kills/segment_count are the plan scalars.
const PARSED_JOB = {
  id: '00000000-0000-4000-8000-000000000000',
  status: 'parsed',
  created_at: '2026-07-11T12:00:00.000Z',
  map: 'de_inferno',
  target_kills: 24,
  segment_count: 5,
};

test.describe('live matches visibility', () => {
  test('an out-of-band job appears on /matches via the poll loop, no reload', async ({ page }) => {
    // The list the mocked proxy returns; mutated mid-test to simulate a job
    // being created server-side after the page has mounted on an empty list.
    let jobs: unknown[] = [];
    await page.route('**/api/demos*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ jobs }),
      }),
    );

    // Mount on an empty orchestrator: the landing empty state, not a stale card.
    await page.goto('/matches');
    await expect(page.getByText('Aún no hay partidas')).toBeVisible();
    await expect(page.getByText('Inferno')).toHaveCount(0);

    // A job is created out of band and reaches `parsed`. No click, no reload:
    // the next poll tick must surface it. The timeout is comfortably above the
    // 5s idle cadence so a single idle beat (then fast, since the id set changed)
    // is enough.
    jobs = [PARSED_JOB];
    await expect(page.getByText('Inferno')).toBeVisible({ timeout: 10_000 });
    // The empty state is gone once a match lists.
    await expect(page.getByText('Aún no hay partidas')).toHaveCount(0);

    mkdirSync(ARTIFACT_DIR, { recursive: true });
    await page.screenshot({ path: resolve(ARTIFACT_DIR, 'matches-live-job.png'), fullPage: true });
  });
});

// Full path against the real pipeline: boot the orchestrator, POST a real .dem,
// and assert the parsed job lists on /matches unaided. Gated on a reachable
// orchestrator and a real demo fixture (ZV_E2E_DEMO, default
// ../testdata/sample.dem) so it skips - never fails - when either is absent, the
// same gating upload.spec.ts uses (a real .dem is never committed to the repo).
const DEMO_PATH = process.env.ZV_E2E_DEMO ?? resolve(__dirname, '../../testdata/sample.dem');
const ORCHESTRATOR = process.env.ZV_E2E_ORCHESTRATOR ?? 'http://127.0.0.1:8080';

test.describe('live matches (real demo + orchestrator)', () => {
  test('a demo parsed by the orchestrator lists on /matches', async ({ page, request }) => {
    test.skip(!existsSync(DEMO_PATH), `no demo fixture at ${DEMO_PATH} (set ZV_E2E_DEMO)`);
    const orchestratorUp = await request
      .get(`${ORCHESTRATOR}/healthz`)
      .then((r) => r.ok())
      .catch(() => false);
    test.skip(!orchestratorUp, `orchestrator not reachable at ${ORCHESTRATOR}`);

    // A ~400 MB upload plus a full parse - generous budget, never the default 30s.
    test.setTimeout(180_000);

    // Create the job straight on the orchestrator (POST /api/jobs, multipart
    // `demo`), the same request the /api/demos/scan proxy makes. With no target
    // the orchestrator runs scan+parse, so the job reaches `parsed` and lists.
    const created = await request.post(`${ORCHESTRATOR}/api/jobs`, {
      multipart: { demo: { name: 'sample.dem', mimeType: 'application/octet-stream', buffer: readFileSync(DEMO_PATH) } },
    });
    expect(created.ok()).toBeTruthy();

    await page.goto('/matches');
    // The parse runs server-side; the poll loop surfaces the card unaided.
    await expect(page.locator('main')).toContainText(/inferno|dust2|mirage|nuke|overpass|ancient|anubis|vertigo/i, {
      timeout: 150_000,
    });
  });
});
