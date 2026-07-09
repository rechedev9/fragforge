import { test, expect, type Page, request } from "@playwright/test";

// Canonical download asset for the live GitHub Release v0.3.4. The primary CTA
// must point at exactly this URL, and the URL must actually resolve.
const DOWNLOAD_URL =
  "https://github.com/rechedev9/fragforge/releases/download/v0.3.4/FragForge.Studio.Setup.0.3.4.exe";

function trackConsole(page: Page) {
  const errors: string[] = [];
  page.on("console", (msg) => {
    if (msg.type() === "error") errors.push(`console.error: ${msg.text()}`);
  });
  page.on("pageerror", (err) => errors.push(`pageerror: ${err.message}`));
  return errors;
}

test.describe("landing page", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("primary download CTA points at the canonical asset and it resolves 200", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "load" });

    const cta = page.getByRole("link", { name: "Download for Windows" });
    await expect(cta).toBeVisible();

    // 1a. The href is byte-for-byte the canonical asset URL.
    const href = await cta.getAttribute("href");
    expect(href).toBe(DOWNLOAD_URL);

    // 1b. The asset actually resolves. HEAD, following redirects to the CDN.
    const ctx = await request.newContext();
    const res = await ctx.head(href!, { maxRedirects: 10 });
    expect(
      res.status(),
      `Expected the download asset to resolve 200, got ${res.status()} (${res.url()})`,
    ).toBe(200);
    await ctx.dispose();
  });

  test("has accessible section headings and the SmartScreen note", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "load" });

    await expect(
      page.getByRole("heading", { name: "What it does" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "How it works" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Requirements" }),
    ).toBeVisible();

    // The unsigned-installer note is present and honest about SmartScreen.
    await expect(
      page.getByText("Windows protected your PC", { exact: false }),
    ).toBeVisible();
  });

  test("loads with zero console/page errors after settling", async ({
    page,
  }) => {
    const errors = trackConsole(page);

    await page.goto("/", { waitUntil: "load" });
    await page.waitForTimeout(3000);

    expect(
      errors,
      `Expected no console/page errors, but got:\n${errors.join("\n")}`,
    ).toEqual([]);
  });
});
