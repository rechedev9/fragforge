import { test, expect } from '@playwright/test';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { pathToFileURL } from 'node:url';

/**
 * F0 reference renderer. Opens the frozen FragForge Anime design HTML and
 * screenshots each NEON HUD screen (1280x800) to .loop/reference/anime/shots/.
 * These PNGs are the visual judge's frozen reference; they are rendered once
 * and not regenerated in later iterations. Run with:
 *   npx playwright test -c e2e/anime-refs.config.ts
 *
 * Network note: this is the only step allowed to reach fonts.googleapis.com /
 * fonts.gstatic.com, so the mockup renders with its real fonts (hard-asserted
 * below; a fallback font invalidates the reference set).
 */

const ANIME_DIR = path.resolve(__dirname, '..', '..', '.loop', 'reference', 'anime');
const HTML_PATH = path.join(ANIME_DIR, 'FragForge-Anime.dc.html');
const OUT = path.join(ANIME_DIR, 'shots');

/** All anchors the frozen document must contain (3b Editor is out of scope for shots). */
const ANCHORS = ['1a', '2a', '2b', '2c', '2d', '2e', '3a', '3b', '3c'];

/** Screens captured by data-screen-label ("Editor" is deliberately excluded). */
const LABELED_SHOTS: Array<{ file: string; label: string }> = [
  { file: '2a-partidas.png', label: 'Partidas' },
  { file: '2b-subir-demo.png', label: 'Subir demo' },
  { file: '2c-clips-stream.png', label: 'Clips de stream' },
  { file: '2d-biblioteca.png', label: 'Biblioteca' },
  { file: '2e-feed.png', label: 'Feed' },
  { file: '3a-detalle.png', label: 'Detalle de partida' },
  { file: '3c-emparejar.png', label: 'Emparejar PC' },
];

test('render frozen NEON HUD mockups to reference PNGs', async ({ page }) => {
  expect(fs.existsSync(HTML_PATH), `frozen mockup missing: ${HTML_PATH}`).toBe(true);
  fs.mkdirSync(OUT, { recursive: true });

  await page.goto(pathToFileURL(HTML_PATH).href);

  // Hard gate: the real display + mono fonts must be loaded, not a fallback.
  await page.evaluate(() => document.fonts.ready);
  const chakraLoaded = await page.evaluate(() => document.fonts.check('16px "Chakra Petch"'));
  const shareTechLoaded = await page.evaluate(() => document.fonts.check('16px "Share Tech Mono"'));
  expect(chakraLoaded, 'Chakra Petch must be loaded (no fallback font in references)').toBe(true);
  expect(shareTechLoaded, 'Share Tech Mono must be loaded (no fallback font in references)').toBe(true);

  // The frozen document must still contain every expected screen anchor.
  for (const anchor of ANCHORS) {
    await expect(page.locator(`[id="${anchor}"]`), `anchor #${anchor} missing`).toHaveCount(1);
  }

  // 7 labeled screens (every data-screen-label except "Editor").
  for (const shot of LABELED_SHOTS) {
    const el = page.locator(`div[data-screen-label="${shot.label}"]`);
    await expect(el, `screen "${shot.label}" missing`).toHaveCount(1);
    await el.scrollIntoViewIfNeeded();
    await el.screenshot({ path: path.join(OUT, shot.file) });
  }

  // Welcome screen 1a has no data-screen-label; it is the option card's screen div.
  const welcome = page.locator('[id="1a"] .dv-card > div').first();
  await expect(welcome, 'welcome screen #1a .dv-card > div missing').toHaveCount(1);
  await welcome.scrollIntoViewIfNeeded();
  await welcome.screenshot({ path: path.join(OUT, '1a-bienvenida.png') });

  // Sanity: 8 PNGs, none suspiciously small (a blank 1280x800 PNG is ~5-10 KB).
  const files = [...LABELED_SHOTS.map((s) => s.file), '1a-bienvenida.png'];
  for (const file of files) {
    const stat = fs.statSync(path.join(OUT, file));
    expect(stat.size, `${file} looks blank (${stat.size} bytes)`).toBeGreaterThan(20_000);
  }
});
