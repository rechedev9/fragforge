import { test, expect } from '@playwright/test';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

/**
 * Stream Clips (/streams): paste a Twitch URL, wait for the source video to be
 * acquired, then edit the layout/facecam/clip ranges and render. These specs
 * mock every /api/streams/* call at the network layer (no orchestrator needed),
 * mirroring e2e/upload.spec.ts's error-messaging pattern, so they are fast and
 * deterministic.
 */

const JOB_ID = '22222222-2222-4222-8222-222222222222';
const URL_INPUT = '#stream-url';
const CLIP_URL = 'https://clips.twitch.tv/SomeAmazingClip';

/**
 * A tiny fake MP4 payload, set straight on the file input. The upload flow
 * only checks it is a File before proxying, so this is enough to reach the
 * create call without shipping real media into the repo.
 */
const DUMMY_MP4 = {
  name: 'clip.mp4',
  mimeType: 'video/mp4',
  buffer: Buffer.from('\x00\x00\x00\x18ftypmp42'),
};

const READY_JOB = {
  id: JOB_ID,
  status: 'ready',
  title: 'Insane clutch',
  probe: { width: 1920, height: 1080, duration_seconds: 42 },
  created_at: new Date().toISOString(),
};

const EDIT_PLAN = {
  schema_version: 1,
  variant: 'streamer-vertical-stack-40-60',
  face_crop: { x: 0.62, y: 0.03, width: 0.34, height: 0.3 },
  gameplay_crop: { x: 0, y: 0, width: 1, height: 1 },
  clips: [{ id: 'clip-1', start_seconds: 0, end_seconds: 20, title: 'Clutch' }],
  captions: { enabled: false, language: 'auto' },
};

async function mockCommonRoutes(page: import('@playwright/test').Page): Promise<void> {
  await page.route('**/api/streams/*/source', (route) =>
    route.fulfill({ status: 200, contentType: 'video/mp4', body: Buffer.from('') }),
  );
  await page.route('**/api/streams/*/edit-plan', (route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON();
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(EDIT_PLAN) });
  });
}

test.describe('stream clips — url submit to editor', () => {
  test('polls acquiring until ready, then shows the editor', async ({ page }) => {
    await mockCommonRoutes(page);

    let statusCalls = 0;
    await page.route('**/api/streams', (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      return route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify({ id: JOB_ID, status: 'acquiring', title: 'Insane clutch', created_at: new Date().toISOString() }),
      });
    });
    await page.route(`**/api/streams/${JOB_ID}`, (route) => {
      statusCalls++;
      const body = statusCalls <= 2 ? { ...READY_JOB, status: 'acquiring' } : READY_JOB;
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
    });

    await page.goto('/streams');
    await page.locator(URL_INPUT).fill(CLIP_URL);
    await page.getByRole('button', { name: 'Fetch clip' }).click();

    await expect(page.getByText('Fetching Insane clutch…')).toBeVisible();

    // Once acquisition finishes the editor renders: layout selector, facecam
    // picker, clip ranges, and the Create Shorts action.
    await expect(page.getByText('Layout', { exact: true })).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole('button', { name: 'Facecam crop region' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create Shorts' })).toBeVisible();
  });

  test('reports the service as offline when the create call returns 503 + code', async ({ page }) => {
    await page.route('**/api/streams', (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      return route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'analysis service unavailable', code: 'service_unavailable' }),
      });
    });

    await page.goto('/streams');
    await page.locator(URL_INPUT).fill(CLIP_URL);
    await page.getByRole('button', { name: 'Fetch clip' }).click();

    await expect(page.getByText('Stream Clips service is offline. Start it and try again.')).toBeVisible();
  });

  test('surfaces the render failure reason distinctly from an outage', async ({ page }) => {
    await mockCommonRoutes(page);

    await page.route('**/api/streams', (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      return route.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify(READY_JOB) });
    });
    await page.route(`**/api/streams/${JOB_ID}`, (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(READY_JOB) }),
    );
    await page.route(`**/api/streams/${JOB_ID}/renders/*`, (route) => {
      if (route.request().method() === 'POST') {
        return route.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify({ status: 'rendering', videos: [] }) });
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'failed', videos: [], error: 'ffmpeg encode failed: unsupported codec' }),
      });
    });

    await page.goto('/streams');
    await page.locator(URL_INPUT).fill(CLIP_URL);
    await page.getByRole('button', { name: 'Fetch clip' }).click();

    await expect(page.getByRole('button', { name: 'Create Shorts' })).toBeVisible({ timeout: 15_000 });
    await page.getByRole('button', { name: 'Create Shorts' }).click();

    await expect(page.getByText('ffmpeg encode failed: unsupported codec')).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText('Stream Clips service is offline. Start it and try again.')).toHaveCount(0);
  });
});

// Regression specs for the upload field-name bug: the browser sends the file
// under the "file" field to our own /api/streams proxy (see lib/api/streams.ts
// createFromFile), and the proxy (web/app/api/streams/route.ts) is responsible
// for renaming it to "video" before forwarding upstream, because the
// orchestrator's POST /api/stream-jobs reads r.FormFile("video")
// (internal/httpapi/stream_handlers.go:64). A regression here previously sent
// "file" all the way upstream, so the orchestrator replied 400
// {"error":"missing video file: http: no such file"}. Playwright's page.route
// intercepts the browser's outgoing request before it reaches our Next.js
// route handler, so it cannot observe the renamed field on the wire; the
// client-side contract is asserted here, and the proxy->orchestrator rename is
// covered end to end by the real-orchestrator happy-path spec below (which
// fails the same way a live user would if the field name regressed).
test.describe('stream clips — MP4 upload', () => {
  test('sends the file to the proxy under the "file" multipart field', async ({ page }) => {
    await mockCommonRoutes(page);

    let capturedFieldNames: string[] = [];
    await page.route('**/api/streams', (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      const raw = route.request().postData() ?? '';
      capturedFieldNames = [...raw.matchAll(/name="([^"]+)"/g)].map((m) => m[1]);
      return route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify({ id: JOB_ID, status: 'uploaded', title: 'clip.mp4', created_at: new Date().toISOString() }),
      });
    });
    await page.route(`**/api/streams/${JOB_ID}`, (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(READY_JOB) }),
    );

    await page.goto('/streams');
    await page.getByRole('button', { name: 'Upload an MP4' }).click();
    await page.locator('input[type=file]').setInputFiles(DUMMY_MP4);

    await expect(page.getByText('Layout', { exact: true })).toBeVisible({ timeout: 15_000 });
    expect(capturedFieldNames).toContain('file');
  });

  test('renders the orchestrator 400 message verbatim when the upload is rejected', async ({ page }) => {
    await page.route('**/api/streams', (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      // Mirrors the orchestrator's real response shape when the upstream
      // multipart field is wrong or missing (stream_handlers.go:64-68).
      return route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'missing video file: http: no such file' }),
      });
    });

    await page.goto('/streams');
    await page.getByRole('button', { name: 'Upload an MP4' }).click();
    await page.locator('input[type=file]').setInputFiles(DUMMY_MP4);

    await expect(page.getByText('missing video file: http: no such file')).toBeVisible();
  });

  test('renders the orchestrator 409 message verbatim (e.g. an unconfigured pipeline)', async ({ page }) => {
    await page.route('**/api/streams', (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      return route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'stream clips pipeline is not configured: yt-dlp not found' }),
      });
    });

    await page.goto('/streams');
    await page.getByRole('button', { name: 'Upload an MP4' }).click();
    await page.locator('input[type=file]').setInputFiles(DUMMY_MP4);

    await expect(page.getByText('stream clips pipeline is not configured: yt-dlp not found')).toBeVisible();
  });
});

// Full happy path against the real pipeline. Gated on a reachable orchestrator
// so it skips - never fails - when one is not running locally.
const ORCHESTRATOR = process.env.ZV_E2E_ORCHESTRATOR ?? 'http://127.0.0.1:8080';
const SAMPLE_MP4 = process.env.ZV_E2E_STREAM_MP4 ?? resolve(__dirname, '../public/sample-reel.mp4');

test.describe('stream clips happy path (real orchestrator)', () => {
  test('uploads an MP4 and reaches the editor', async ({ page, request }) => {
    test.skip(!existsSync(SAMPLE_MP4), `no sample mp4 at ${SAMPLE_MP4}`);
    const orchestratorUp = await request
      .get(`${ORCHESTRATOR}/healthz`)
      .then((r) => r.ok())
      .catch(() => false);
    test.skip(!orchestratorUp, `orchestrator not reachable at ${ORCHESTRATOR}`);

    test.setTimeout(120_000);

    await page.goto('/streams');
    await page.getByRole('button', { name: 'Upload an MP4' }).click();
    await page.locator('input[type=file]').setInputFiles(SAMPLE_MP4);

    await expect(page.getByText('Layout')).toBeVisible({ timeout: 90_000 });
  });
});
