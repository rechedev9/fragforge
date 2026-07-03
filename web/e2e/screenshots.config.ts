import { defineConfig } from '@playwright/test';

/**
 * Config for the visual-judge screenshot harness (F0). Single command:
 *   npx playwright test -c e2e/screenshots.config.ts
 *
 * Boots its own Next dev server on port 3200 (NEVER 3000/3100) with
 * NEXT_PUBLIC_API_BASE forced empty so web/lib/api picks the in-memory mock
 * regardless of web/.env.local, and cloud (default) mode so `/` and `/connect`
 * render instead of redirecting.
 */
export default defineConfig({
  testDir: '.',
  testMatch: 'screenshots.ts',
  workers: 1,
  reporter: 'list',
  timeout: 120_000,
  // The Next dev server on Windows can stall route data briefly while it
  // compiles; give content waits a generous margin (this is a capture harness,
  // not a perf test).
  expect: { timeout: 20_000 },
  use: {
    baseURL: 'http://localhost:3200',
    viewport: { width: 1280, height: 800 },
    deviceScaleFactor: 1,
  },
  webServer: {
    command: 'npm run dev -- -p 3200',
    url: 'http://localhost:3200',
    reuseExistingServer: false,
    timeout: 180_000,
    env: {
      ...(process.env as Record<string, string>),
      NEXT_PUBLIC_API_BASE: '',
      NEXT_PUBLIC_FRAGFORGE_MODE: 'cloud',
    },
  },
});
