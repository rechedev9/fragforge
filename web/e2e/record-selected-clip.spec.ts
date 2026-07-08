import { test, expect } from '@playwright/test';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

// Verifies the whole frontend reel flow records ONLY the user-selected clip
// instead of the entire demo: upload -> pick player -> select one clip -> create
// reel -> (record + render) -> ready, then asserts exactly ONE of the match's
// segments produced a rendered video. Gated on a reachable host orchestrator
// with capture + render enabled and a real multi-clip demo (ZV_E2E_DEMO), so it
// skips - never fails - when those are absent. With ZV_RECORDER_FAKE=1 on the
// orchestrator it completes in seconds without launching CS2/HLAE.
const DEMO = process.env.ZV_E2E_DEMO ?? resolve(__dirname, '../../testdata/sample.dem');
const ORCHESTRATOR = process.env.ZV_E2E_ORCHESTRATOR ?? 'http://127.0.0.1:8080';

test('reel records only the selected clip, not the whole demo', async ({ page, request }) => {
  test.skip(!existsSync(DEMO), `no demo fixture at ${DEMO} (set ZV_E2E_DEMO)`);
  const caps = await request
    .get(`${ORCHESTRATOR}/api/capabilities`)
    .then((r) => (r.ok() ? r.json() : null))
    .catch(() => null);
  test.skip(!caps?.record?.enabled || !caps?.render?.enabled, `orchestrator at ${ORCHESTRATOR} has no capture+render worker`);

  test.setTimeout(900_000);
  await page.setViewportSize({ width: 1280, height: 1400 });

  await page.goto('/upload');
  await page.locator('input[type=file]').setInputFiles(DEMO);
  await page.getByRole('heading', { name: '¿A QUIÉN QUIERES CLIPEAR?' }).waitFor({ timeout: 180_000 });
  await page.getByTestId('player-avatar').first().click();
  await page.waitForURL(/\/matches\//, { timeout: 180_000 });
  const jobId = page.url().split('/matches/')[1].split(/[/?#]/)[0];
  console.log('[pipeline] jobId=' + jobId);
  await page.getByText(/JUGADAS DETECTADAS/).waitFor({ timeout: 180_000 });

  // Every kill-plan segment id for this job (the clips the user could pick).
  const plan = (await page.request.get(`/api/demos/${jobId}/plan`).then((r) => r.json())) as {
    segments?: { id: string }[];
  };
  const segmentIds = (plan.segments ?? []).map((s) => s.id);
  console.log('[pipeline] plan segments=' + segmentIds.length + ' ' + JSON.stringify(segmentIds));
  // A meaningful "only one" assertion needs a demo with more than one clip.
  test.skip(segmentIds.length < 2, 'demo has fewer than 2 clips; cannot assert single-clip scoping');

  // Pick exactly ONE clip and create its reel.
  await page.locator('button:has(.lucide-crosshair)').first().click();
  await page.getByRole('button', { name: 'FORJAR REEL' }).click();
  await page.waitForURL(/\/videos/, { timeout: 60_000 });
  console.log('[pipeline] reel created, capture should start');

  const variant = 'viral-60-clean';
  type RV = { status?: string; failure_reason?: string };
  const getStatus = (): Promise<RV> =>
    page.request
      .get(`/api/demos/${jobId}/renders/${variant}`)
      .then((r) => (r.ok() ? (r.json() as Promise<RV>) : ({} as RV)))
      .catch(() => ({} as RV));

  const deadline = Date.now() + 840_000;
  let last = '';
  let final = '';
  while (Date.now() < deadline) {
    const rv = await getStatus();
    if (rv.status && rv.status !== last) {
      console.log('[pipeline] render=' + rv.status);
      last = rv.status;
    }
    if (rv.status === 'ready') {
      final = 'ready';
      break;
    }
    if (rv.status === 'failed') {
      final = 'failed:' + (rv.failure_reason || '?');
      break;
    }
    await page.waitForTimeout(3000);
  }
  console.log('[pipeline] final=' + final);
  expect(final).toBe('ready');

  // Core assertion: exactly ONE segment was recorded+rendered (the selected
  // clip), not the whole demo. Count which segment videos actually exist.
  let rendered = 0;
  for (const id of segmentIds) {
    const r = await page.request.get(`/api/demos/${jobId}/renders/${variant}/videos/${id}`);
    if (r.ok()) rendered++;
  }
  console.log('[pipeline] rendered segment videos=' + rendered + ' of ' + segmentIds.length);
  expect(rendered).toBe(1);
});
