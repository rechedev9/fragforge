import { test, expect } from '@playwright/test';

/**
 * Regression spec for library deletion: every reel card offers a Delete that
 * asks for confirmation and removes the reel from the Library. Runs against
 * the mock seed videos, so it needs only the dev server.
 */
test.describe('library delete', () => {
  test('deletes a ready reel after confirmation', async ({ page }) => {
    await page.goto('/videos');

    const readySection = page.locator('section', { hasText: 'LISTOS' }).last();
    const firstCard = readySection.locator('[data-slot="card"]').first();
    await expect(firstCard).toBeVisible();
    const title = (await firstCard.locator('p.truncate').first().textContent()) ?? '';
    expect(title).not.toBe('');

    await firstCard.getByRole('button', { name: `Borrar ${title}` }).click();
    await expect(page.getByText('¿Borrar este reel?')).toBeVisible();
    await page.getByRole('button', { name: 'Borrar', exact: true }).click();

    await expect(page.getByText('¿Borrar este reel?')).toHaveCount(0);
    await expect(readySection.getByText(title, { exact: true })).toHaveCount(0);
  });

  test('cancel keeps the reel', async ({ page }) => {
    await page.goto('/videos');

    const readySection = page.locator('section', { hasText: 'LISTOS' }).last();
    const firstCard = readySection.locator('[data-slot="card"]').first();
    await expect(firstCard).toBeVisible();
    const title = (await firstCard.locator('p.truncate').first().textContent()) ?? '';

    await firstCard.getByRole('button', { name: `Borrar ${title}` }).click();
    await page.getByRole('button', { name: 'Cancelar' }).click();

    await expect(page.getByText('¿Borrar este reel?')).toHaveCount(0);
    await expect(readySection.getByText(title, { exact: true }).first()).toBeVisible();
  });
});
