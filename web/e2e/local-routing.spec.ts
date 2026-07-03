import { test, expect } from '@playwright/test';

/**
 * Regression specs for local-studio routing: in local mode the dashboard
 * (/matches) is home. The cloud landing at "/" has a Steam login that does not
 * exist locally, so nothing may strand the desktop user there.
 *
 * NEXT_PUBLIC_FRAGFORGE_MODE is baked into the client bundle at build time, so
 * these specs only make sense against a dev server started in local mode:
 *
 *   NEXT_PUBLIC_FRAGFORGE_MODE=local npm run dev
 *   NEXT_PUBLIC_FRAGFORGE_MODE=local npx playwright test e2e/local-routing.spec.ts
 *
 * They skip (rather than fail) when the suite runs in the default cloud mode.
 */
const localMode = process.env.NEXT_PUBLIC_FRAGFORGE_MODE === 'local';

test.describe('local studio routing', () => {
  test.skip(!localMode, 'needs a dev server built with NEXT_PUBLIC_FRAGFORGE_MODE=local');

  test('the cloud landing at / redirects to the dashboard', async ({ page }) => {
    await page.goto('/');
    await page.waitForURL('**/matches');
    await expect(page.getByRole('heading', { name: 'TUS PARTIDAS' })).toBeVisible();
  });

  test('Volver from the upload flow returns to the dashboard, not the landing', async ({ page }) => {
    await page.goto('/upload');
    await page.getByRole('link', { name: 'Volver' }).click();
    await page.waitForURL('**/matches');
    await expect(page.getByRole('heading', { name: 'TUS PARTIDAS' })).toBeVisible();
    // The Steam-login landing must never flash in between.
    expect(page.url()).not.toMatch(/\/$/);
  });

  test('the empty dashboard routes into both content flows', async ({ page }) => {
    await page.goto('/matches');
    await expect(page.getByText('Aún no hay partidas')).toBeVisible();

    await page.getByRole('link', { name: 'ANALIZAR UNA DEMO' }).click();
    await page.waitForURL('**/upload');

    await page.goto('/matches');
    // Scope to the page body; the sidebar nav links to /streams too (as
    // "CLIPS DE STREAM"), so keep this pinned to the in-page link.
    await page.getByRole('main').getByRole('link', { name: 'CLIPS DE STREAM' }).click();
    await page.waitForURL('**/streams');
  });
});
