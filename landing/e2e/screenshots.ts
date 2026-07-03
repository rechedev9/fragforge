import { test } from "@playwright/test";
import * as fs from "node:fs";
import * as path from "node:path";

// F4 judge capture script. Single command:
//   npx playwright test e2e/screenshots.ts
// Produces PNGs under e2e/screenshots/ (gitignored): one top-of-page shot and
// one per section, at exactly 1440x900 (desktop) and 390x844 (mobile).

const OUT = path.join(__dirname, "screenshots");
const SECTIONS = ["#hero", "#what-it-does", "#how-it-works", "#requirements", "#smartscreen", "footer"];

const VIEWPORTS = [
  { name: "desktop", width: 1440, height: 900 },
  { name: "mobile", width: 390, height: 844 },
] as const;

test.beforeAll(() => {
  fs.mkdirSync(OUT, { recursive: true });
});

for (const vp of VIEWPORTS) {
  test(`capture ${vp.name} ${vp.width}x${vp.height}`, async ({ browser }) => {
    test.setTimeout(120000);
    const context = await browser.newContext({
      viewport: { width: vp.width, height: vp.height },
    });
    const page = await context.newPage();
    await page.goto("/", { waitUntil: "networkidle" });
    await page.waitForTimeout(4000); // let the particle forge reach steady state
    await page.screenshot({ path: path.join(OUT, `${vp.name}-top.png`) });
    for (const sel of SECTIONS) {
      const el = page.locator(sel).first();
      if ((await el.count()) === 0) continue;
      await el.scrollIntoViewIfNeeded();
      await page.waitForTimeout(700);
      const name = sel.replace(/^#/, "");
      await page.screenshot({ path: path.join(OUT, `${vp.name}-${name}.png`) });
    }
    await context.close();
  });
}
