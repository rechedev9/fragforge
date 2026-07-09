import { test, expect } from '@playwright/test';

/**
 * Regression specs for local-studio routing: the dashboard (/matches) is
 * home, and the root "/" is just a redirect to it.
 */
test.describe('local studio routing', () => {
  test('the root redirects to the dashboard', async ({ page }) => {
    await page.goto('/');
    await page.waitForURL('**/matches');
    await expect(page.getByRole('heading', { name: 'TUS PARTIDAS' })).toBeVisible();
  });

  test('Volver from the upload flow returns to the dashboard, not the landing', async ({ page }) => {
    await page.goto('/upload');
    await page.getByRole('link', { name: 'Volver' }).click();
    await page.waitForURL('**/matches');
    await expect(page.getByRole('heading', { name: 'TUS PARTIDAS' })).toBeVisible();
    // The root redirect must never flash in between.
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
