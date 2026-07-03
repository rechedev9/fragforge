import { defineConfig } from "@playwright/test";

// Config for the F4 judge capture script only. Single command:
//   npx playwright test -c e2e/screenshots.config.ts
export default defineConfig({
  testDir: ".",
  testMatch: "screenshots.ts",
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://localhost:3100",
  },
  webServer: {
    command: "npm run build && npm run start",
    url: "http://localhost:3100",
    reuseExistingServer: true,
    timeout: 180000,
  },
});
