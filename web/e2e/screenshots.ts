import { test, expect, type Page } from '@playwright/test';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

/**
 * F0 visual-judge capture harness. Single command:
 *   npx playwright test -c e2e/screenshots.config.ts
 *
 * Captures every app surface (mock API, cloud mode, port 3200) as fullPage
 * PNGs under e2e/screenshots/ (gitignored). Every capture waits on a
 * screen-specific content selector, never on networkidle alone.
 *
 * PNGs are staged in a temp dir OUTSIDE the project during the run and copied
 * into e2e/screenshots/ only after the last test: writing files under web/
 * while the dev server runs triggers Next Fast Refresh rebuilds, which can
 * strand the next page load before hydration (its mock data then never loads).
 */

const OUT = path.join(__dirname, 'screenshots');
let stageDir: string;

// A fixture match id from web/lib/api/fixtures.ts (fixtureMatches[0]).
const MATCH_ID = 'm-inferno';
// Seed video title from web/lib/api/fixtures.ts seedVideos(); its ready card
// carries the delete button aria-label `Delete ${title}`.
const READY_VIDEO_TITLE = '5K - Clean POV';

test.beforeAll(() => {
  stageDir = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-shots-'));
});

test.afterAll(() => {
  fs.mkdirSync(OUT, { recursive: true });
  for (const file of fs.readdirSync(stageDir)) {
    fs.copyFileSync(path.join(stageDir, file), path.join(OUT, file));
  }
  fs.rmSync(stageDir, { recursive: true, force: true });
});

async function shoot(page: Page, name: string): Promise<void> {
  // The Next dev-tools indicator (<nextjs-portal>) is dev-only chrome, not app
  // UI; hide it so the visual judge never sees it.
  await page.addStyleTag({ content: 'nextjs-portal { display: none !important; }' });
  // Fast-forward CSS animations/transitions (dialog fade-in, skeleton pulse)
  // so captures are deterministic and fully opaque.
  await page.screenshot({ path: path.join(stageDir, name), fullPage: true, animations: 'disabled' });
}

test('home (/)', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: /forge your frags/i })).toBeVisible();
  await expect(page.getByRole('link', { name: /upload a demo/i })).toBeVisible();
  // Let the three.js hero reel reach a steady frame before capturing.
  await page.waitForTimeout(2000);
  await shoot(page, 'home.png');
});

test('upload (/upload)', async ({ page }) => {
  await page.goto('/upload');
  await expect(page.getByRole('heading', { name: /analyze any demo/i })).toBeVisible();
  await shoot(page, 'upload.png');
});

test('connect (/connect)', async ({ page }) => {
  await page.goto('/connect');
  await expect(page.getByRole('heading', { name: /set up your studio/i })).toBeVisible();
  await shoot(page, 'connect.png');
});

test('matches (/matches)', async ({ page }) => {
  await page.goto('/matches');
  await expect(page.getByRole('heading', { name: 'Matches' })).toBeVisible();
  await expect(page.locator(`a[href="/matches/${MATCH_ID}"]`).first()).toBeVisible();
  await shoot(page, 'matches.png');
});

test('match detail (/matches/[id])', async ({ page }) => {
  await page.goto(`/matches/${MATCH_ID}`);
  await expect(page.getByRole('heading', { name: /we found/i })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Inferno' })).toBeVisible();
  await shoot(page, 'match-detail.png');
});

test('streams (/streams)', async ({ page }) => {
  await page.goto('/streams');
  await expect(page.getByRole('heading', { name: 'Stream Clips' })).toBeVisible();
  await shoot(page, 'streams.png');
});

test('videos (/videos)', async ({ page }) => {
  await page.goto('/videos');
  await expect(page.getByRole('heading', { name: 'Library' })).toBeVisible();
  await expect(page.getByText(READY_VIDEO_TITLE).first()).toBeVisible();
  await shoot(page, 'videos.png');
});

test('feed (/feed)', async ({ page }) => {
  await page.goto('/feed');
  await expect(page.getByRole('heading', { name: 'Feed' })).toBeVisible();
  await expect(page.getByText('RaiSeNN').first()).toBeVisible();
  await shoot(page, 'feed.png');
});

test('dialog + toast (delete-reel flow on /videos)', async ({ page }) => {
  await page.goto('/videos');
  await expect(page.getByText(READY_VIDEO_TITLE).first()).toBeVisible();

  // Open the delete-reel confirmation dialog on the seeded ready video.
  await page.getByRole('button', { name: `Delete ${READY_VIDEO_TITLE}` }).click();
  await expect(page.getByRole('heading', { name: 'Delete this reel?' })).toBeVisible();
  await shoot(page, 'dialog.png');

  // Confirm: the dialog closes and a real sonner toast fires ("Reel deleted.").
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByText('Reel deleted.')).toBeVisible();
  await shoot(page, 'toast.png');
});

test('skeleton (loading state on /matches)', async ({ page }) => {
  // The mock API resolves after an artificial 150-400ms setTimeout; stretch
  // exactly that window so the loading skeleton stays on screen to capture.
  await page.addInitScript(() => {
    const original = window.setTimeout.bind(window);
    const patched = ((handler: TimerHandler, ms?: number, ...args: unknown[]) =>
      original(
        handler,
        typeof ms === 'number' && ms >= 100 && ms <= 500 ? 8000 : ms,
        ...args,
      )) as typeof window.setTimeout;
    window.setTimeout = patched;
  });
  await page.goto('/matches');
  await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible();
  await shoot(page, 'skeleton.png');
});

test('not found (/definitely-missing-route)', async ({ page }) => {
  await page.goto('/definitely-missing-route');
  await expect(page.getByRole('heading', { name: /this page got fragged/i })).toBeVisible();
  await expect(page.getByText('404')).toBeVisible();
  await shoot(page, 'not-found.png');
});

test.describe('mobile 390x844', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('mobile matches', async ({ page }) => {
    await page.goto('/matches');
    await expect(page.getByRole('heading', { name: 'Matches' })).toBeVisible();
    await expect(page.locator(`a[href="/matches/${MATCH_ID}"]`).first()).toBeVisible();
    await shoot(page, 'mobile-matches.png');
  });

  test('mobile match detail', async ({ page }) => {
    await page.goto(`/matches/${MATCH_ID}`);
    await expect(page.getByRole('heading', { name: /we found/i })).toBeVisible();
    await shoot(page, 'mobile-match-detail.png');
  });
});
