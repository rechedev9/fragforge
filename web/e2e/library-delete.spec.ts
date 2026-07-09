import { test, expect } from '@playwright/test';

const JOB_ID = '00000000-0000-4000-8000-0000000000dd';
const SEGMENT_ID = 'seg-001';
const VARIANT = 'viral-60-clean';
const ARTIFACT_NAME = 'library-delete-ready';
const VIDEO_TITLE = 'Ace de Mirage - Viral 60 Clean';
const STORE_KEY = 'fragforge.reels.v1';

/**
 * Regression spec for library deletion: every reel card offers a Delete that
 * asks for confirmation and removes the reel from the Library. The fixture
 * seeds one durable local reel intent and mocks the two read-only status calls
 * that reconcile it to ready, so it needs only the dev server.
 *
 * The Library renders every non-failed reel in one flat grid (no per-status
 * section headers), and `[data-slot="card"]` is unique to the ready-reel
 * card (RenderingCard/FailedCard do not carry it), so it alone is enough to
 * scope onto the ready card without a "LISTOS" section wrapper.
 */
test.describe('library delete', () => {
  test.beforeEach(async ({ page }) => {
    const intent = {
      videoId: `${JOB_ID}__${SEGMENT_ID}`,
      jobId: JOB_ID,
      segmentIds: [SEGMENT_ID],
      mode: 'clean',
      variant: VARIANT,
      editConfig: {
        format: 'short-9x16',
        killEffect: 'punch-in',
        transition: 'flash',
        intro: false,
        outro: false,
        introText: '',
        outroText: '',
      },
      title: VIDEO_TITLE,
      map: 'de_mirage',
      score: '13-9',
      createdAt: 1_720_000_000_000,
      published: false,
    };
    await page.addInitScript(
      ({ key, reel }) => window.localStorage.setItem(key, JSON.stringify([reel])),
      { key: STORE_KEY, reel: intent },
    );
    await page.route(`**/api/demos/${JOB_ID}/status`, (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'done' }) }),
    );
    await page.route(`**/api/demos/${JOB_ID}/renders/${VARIANT}`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'ready', videos: [ARTIFACT_NAME], covers: [ARTIFACT_NAME] }),
      }),
    );
    await page.route(`**/api/demos/${JOB_ID}/renders/${VARIANT}/videos/${ARTIFACT_NAME}`, (route) =>
      route.fulfill({ status: 204 }),
    );
  });

  test('deletes a ready reel after confirmation', async ({ page }) => {
    await page.goto('/videos');

    const firstCard = page.locator('[data-slot="card"]').first();
    await expect(firstCard).toBeVisible();
    const title = (await firstCard.locator('p.truncate').first().textContent()) ?? '';
    expect(title).not.toBe('');

    await firstCard.getByRole('button', { name: `Borrar ${title}` }).click();
    await expect(page.getByText('¿Borrar este reel?')).toBeVisible();
    await page.getByRole('button', { name: 'Borrar', exact: true }).click();

    await expect(page.getByText('¿Borrar este reel?')).toHaveCount(0);
    await expect(page.getByText(title, { exact: true })).toHaveCount(0);
  });

  test('cancel keeps the reel', async ({ page }) => {
    await page.goto('/videos');

    const firstCard = page.locator('[data-slot="card"]').first();
    await expect(firstCard).toBeVisible();
    const title = (await firstCard.locator('p.truncate').first().textContent()) ?? '';

    await firstCard.getByRole('button', { name: `Borrar ${title}` }).click();
    await page.getByRole('button', { name: 'Cancelar' }).click();

    await expect(page.getByText('¿Borrar este reel?')).toHaveCount(0);
    await expect(page.getByText(title, { exact: true }).first()).toBeVisible();
  });
});
