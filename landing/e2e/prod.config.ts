import { defineConfig, devices } from "@playwright/test";

// Runs the full e2e suite against a deployed URL instead of the local server:
//   PROD_URL=https://fragforge-landing.vercel.app npx playwright test -c e2e/prod.config.ts
if (!process.env.PROD_URL) {
  throw new Error("set PROD_URL to the deployment to verify");
}

export default defineConfig({
  testDir: ".",
  testMatch: "**/*.spec.ts",
  workers: 1,
  reporter: "list",
  use: {
    baseURL: process.env.PROD_URL,
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
