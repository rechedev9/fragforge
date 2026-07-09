import { expect, test } from '@playwright/test';

test.describe('Studio keyboard accessibility', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/matches');
  });

  test('collapsed navigation links keep accessible names', async ({ page }) => {
    await page.keyboard.press('Control+b');

    await expect(page.getByRole('link', { name: 'Ir a Partidas' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Partidas', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Subir demo', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Clips de stream', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Biblioteca', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Feed', exact: true })).toBeVisible();
  });

  test('capture dialog restores focus to the trigger that opened it', async ({ page }) => {
    const trigger = page.getByRole('button', { name: /^Captura:/ });
    await trigger.click();
    await expect(page.getByRole('heading', { name: 'Captura de gameplay' })).toBeVisible();

    await page.keyboard.press('Escape');

    await expect(page.getByRole('heading', { name: 'Captura de gameplay' })).toHaveCount(0);
    await expect(trigger).toBeFocused();
  });
});
