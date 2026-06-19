// design-sync helper: compile the app's Tailwind v4 stylesheet (app/globals.css
// — tokens, @theme, utilities, custom classes) into a static CSS file the DS
// bundle can ship. Run from web/:  node .design-sync/compile-css.mjs
// Output lands in the stub package dir so cfg.cssEntry containment passes.
import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
// Resolve from web/node_modules (the app's own deps).
const postcss = require('postcss');
const tailwind = require('@tailwindcss/postcss');

const INPUT = 'app/globals.css';
const OUT_DIR = 'node_modules/cs2video-web';
const OUT = `${OUT_DIR}/styles.css`;

// Explicit sources so every class the DS ships is generated, regardless of
// .gitignore (the built bundle is gitignored but carries cva-composed classes).
// Scan components + the real app pages (so the design agent's layout vocabulary
// ships, not just component-internal classes) + authored previews + the bundle.
const sources = ['components', 'app', 'app/globals.css']
  .concat(existsSync('.design-sync/previews') ? ['.design-sync/previews'] : [])
  .concat(existsSync('ds-bundle/_ds_bundle.js') ? ['ds-bundle/_ds_bundle.js'] : []);
const sourceDirectives = sources.map((s) => `@source "${process.cwd().split('\\').join('/')}/${s}";`).join('\n');

const input = `${sourceDirectives}\n${readFileSync(INPUT, 'utf8')}`;

const result = await postcss([tailwind({ base: process.cwd() })]).process(input, {
  from: INPUT,
  to: OUT,
});

// Brand fonts. The app wires Space Grotesk / Inter / JetBrains Mono via
// next/font CSS vars that don't exist in the standalone DS bundle, and
// `@theme inline` doesn't emit --font-* as :root vars — so define them
// directly and load the families from Google Fonts (loaded at runtime in the
// real render environment; previews fall back to system fonts offline).
const FONT_IMPORT =
  "@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&family=Space+Grotesk:wght@400;500;600;700&display=swap');\n";
const FONT_VARS = `
:root {
  --font-inter: 'Inter';
  --font-space-grotesk: 'Space Grotesk';
  --font-jetbrains-mono: 'JetBrains Mono';
  --font-sans: 'Inter', ui-sans-serif, system-ui, sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, 'JetBrains Mono', monospace;
  --font-display: 'Space Grotesk', 'Inter', ui-sans-serif, sans-serif;
}
`;

// @import must lead the file; font var defs append so they win the cascade.
const css = FONT_IMPORT + result.css + FONT_VARS;

mkdirSync(OUT_DIR, { recursive: true });
writeFileSync(OUT, css);
console.error(`compiled ${INPUT} -> ${OUT} (${(result.css.length / 1024).toFixed(0)} KB) from sources: ${sources.join(', ')}`);
