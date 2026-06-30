import { defineConfig, devices } from '@playwright/test';

/**
 * E2E config for the FragForge web app.
 *
 * Tests run against the Next dev server on :3000 (reused if already running,
 * else started by `webServer`). The error-messaging specs mock the /api/demos
 * routes, so they need only the dev server. The upload happy-path spec also
 * needs the local orchestrator on :8080 and a real .dem at ZV_E2E_DEMO (default
 * ../testdata/sample.dem); it skips itself when either is absent, so the suite
 * still passes in CI without a 400 MB demo or a running orchestrator.
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: 'list',
  use: {
    baseURL: process.env.ZV_E2E_BASE_URL ?? 'http://localhost:3000',
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run dev',
    url: 'http://localhost:3000',
    reuseExistingServer: true,
    timeout: 120_000,
  },
});
