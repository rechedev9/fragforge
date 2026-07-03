import { test, expect } from "@playwright/test";

test("home page loads with no console errors and shows an h1", async ({
  page,
}) => {
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];

  page.on("console", (msg) => {
    if (msg.type() === "error") {
      consoleErrors.push(msg.text());
    }
  });
  page.on("pageerror", (err) => {
    pageErrors.push(err.message);
  });

  await page.goto("/", { waitUntil: "load" });

  // Let the page settle so late-firing errors are captured.
  await page.waitForTimeout(3000);

  const collected = [
    ...consoleErrors.map((m) => `console.error: ${m}`),
    ...pageErrors.map((m) => `pageerror: ${m}`),
  ];

  expect(
    collected,
    `Expected no console errors or page errors during load, but got:\n${collected.join(
      "\n",
    )}`,
  ).toEqual([]);

  await expect(page.locator("h1")).toBeVisible();
});
