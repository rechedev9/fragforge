import { test, expect, type Page } from "@playwright/test";

function trackConsole(page: Page) {
  const errors: string[] = [];
  page.on("console", (msg) => {
    if (msg.type() === "error") errors.push(`console.error: ${msg.text()}`);
  });
  page.on("pageerror", (err) => errors.push(`pageerror: ${err.message}`));
  return errors;
}

test.describe("hero forge — desktop, motion allowed", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("mounts a sized canvas in the hero with no console errors over 5s", async ({
    page,
  }) => {
    const errors = trackConsole(page);

    await page.goto("/", { waitUntil: "load" });

    const heroArt = page.getByTestId("hero-art");
    await expect(heroArt).toBeVisible();
    const artSize = await heroArt.evaluate((el) => ({
      width: (el as HTMLImageElement).naturalWidth,
      height: (el as HTMLImageElement).naturalHeight,
    }));
    expect(artSize.width).toBeGreaterThan(1000);
    expect(artSize.height).toBeGreaterThan(500);

    const canvas = page.locator("#hero canvas");
    await canvas.waitFor({ state: "attached", timeout: 15000 });

    // 5s settle so late-firing WebGL/postprocessing errors are captured.
    await page.waitForTimeout(5000);

    const size = await canvas.evaluate((el) => ({
      w: (el as HTMLCanvasElement).clientWidth,
      h: (el as HTMLCanvasElement).clientHeight,
    }));
    expect(size.w).toBeGreaterThan(0);
    expect(size.h).toBeGreaterThan(0);

    expect(
      errors,
      `Expected no console/page errors, but got:\n${errors.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("hero forge — reduced motion", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("renders the static fallback, no canvas, h1 visible, no errors", async ({
    page,
  }) => {
    // Must be set BEFORE navigation so the client reads it on first mount.
    await page.emulateMedia({ reducedMotion: "reduce" });
    const errors = trackConsole(page);

    await page.goto("/", { waitUntil: "load" });
    await page.waitForTimeout(3000);

    await expect(page.locator("#hero canvas")).toHaveCount(0);
    await expect(page.getByTestId("hero-forge")).toBeVisible();
    await expect(page.getByTestId("hero-art")).toBeVisible();
    await expect(page.locator("h1")).toBeVisible();

    expect(
      errors,
      `Expected no console/page errors, but got:\n${errors.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("hero forge — mobile DPR cap", () => {
  test.use({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 3 });

  test("caps the canvas drawing-buffer DPR at <= 2 despite deviceScaleFactor 3", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "load" });

    const canvas = page.locator("#hero canvas");
    await canvas.waitFor({ state: "attached", timeout: 15000 });
    await page.waitForTimeout(2000);

    const dpr = await canvas.evaluate((el) => {
      const c = el as HTMLCanvasElement;
      return c.width / c.clientWidth;
    });

    expect(dpr).toBeGreaterThan(0);
    expect(dpr).toBeLessThanOrEqual(2);

    const layout = await page.evaluate(() => ({
      viewport: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
    }));
    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.viewport);
  });
});
