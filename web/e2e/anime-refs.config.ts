import { defineConfig } from '@playwright/test';

/**
 * One-off config for rendering the frozen NEON HUD mockup HTML
 * (.loop/reference/anime/FragForge-Anime.dc.html) to reference PNGs.
 * No webServer: the spec opens the HTML via a file:// URL. Run with:
 *   npx playwright test -c e2e/anime-refs.config.ts
 */
export default defineConfig({
  testDir: '.',
  testMatch: 'anime-refs.ts',
  workers: 1,
  reporter: 'list',
  timeout: 120_000,
  use: {
    // Deterministic 1x rendering: the mockup screens are exactly 1280x800.
    viewport: { width: 1600, height: 1000 },
    deviceScaleFactor: 1,
  },
});
