# design-sync notes — FragForge web DS (cs2video-web)

Repo-specific gotchas for syncing `web/` (Next.js 15 + React 19 + shadcn/ui +
Tailwind v4) to claude.ai/design. Read this before re-syncing.

## Source shape: synth-entry (no build)

`web/` is a Next.js **app**, not a published component library — there is no
`dist/`. The converter runs in **synth-entry mode** (bundles directly from
`web/components/*.tsx`).

- `cfg.srcDir = "../../components"` and the stub package dir
  `node_modules/cs2video-web/package.json` (`{name, version}`, no entry) are
  what make synth-entry work: `PKG_DIR = node_modules/cs2video-web` must EXIST
  (else `exportedNames`/`projectFor` crash reading its package.json) but must
  have **no** `module`/`main`/`exports` (else it's treated as a real dist).
  **`node_modules/` is gitignored — recreate the stub on a fresh clone:**
  `mkdir -p node_modules/cs2video-web && printf '{"name":"cs2video-web","version":"0.1.0","private":true}' > node_modules/cs2video-web/package.json`
- Consequence: there is **no `.d.ts` source**, so emitted `<Name>.d.ts` prop
  bodies are empty/thin. Usage guidance for the design agent comes from the
  authored previews' `.prompt.md` examples instead. (Future improvement: add
  `cfg.dtsPropsFor` for key components, or generate a real declaration build.)

## esbuild shims (cfg.tsconfig = .design-sync/tsconfig.bundle.json)

The bundle must stand alone outside Next's runtime. `tsconfig.bundle.json`
`paths` redirect (via the converter's tsconfigPathsPlugin) to local shims:

- `next/link` → `shims/next-link.tsx` (plain `<a>`)
- `next/navigation` → `shims/next-navigation.ts` (no-op router/hooks)
- `next-themes` → `shims/next-themes.tsx` (fixed dark theme, passthrough provider)
- `@/lib/api` → `shims/lib-api.ts` (always MockApiClient so standalone design
  previews use deterministic fixtures without a running local orchestrator).

The `@/*` alias is intentionally NOT in `tsconfig.bundle.json` — esbuild's
native discovery of `web/tsconfig.json` resolves `@/` (the plugin's `''`-ext
rule wrongly resolves alias-to-directory imports like `@/components/brand` to
the dir instead of its index). Do NOT add `@/*` to the bundle tsconfig.

GOTCHA: the plugin strips `//` comments before JSON.parse — a `"//"` comment KEY
breaks parsing and silently disables the whole plugin. Keep the bundle tsconfig
comment-free.

## CSS: Tailwind v4 must be compiled

Component styles are Tailwind utility classes; the bundle ships class names with
no stylesheet unless compiled. `node .design-sync/compile-css.mjs` compiles
`app/globals.css` (tokens + @theme + utilities + custom .fragforge-* classes)
scanning `components/`, the previews, and the built bundle, writing
`node_modules/cs2video-web/styles.css` (inside the stub pkg so `cfg.cssEntry`
containment passes). **Re-run compile-css.mjs whenever components or previews
change classes, then rebuild.** Order: compile-css → package-build.

## Fonts

Brand fonts (Space Grotesk display, Inter body, JetBrains Mono) are wired in the
app via next/font CSS vars that don't exist standalone, and `@theme inline`
doesn't emit `--font-*` as :root vars. compile-css.mjs defines the `--font-*`
vars directly and loads the families via a Google Fonts `@import`
([FONT_REMOTE] — loads at runtime in the real render env; headless previews fall
back to system fonts, which is fine for grading).

## Authored-preview pattern

Each `.design-sync/previews/<Name>.tsx`:
- `import { X } from 'cs2video-web'` → resolves to `window.FragForge`. Excluded
  sub-parts (CardHeader, DialogContent, SidebarProvider, etc.) are still
  importable this way (they ship in the bundle, just no standalone card).
- `import { Icon } from 'lucide-react'` works (bundled from node_modules).
- Wrap every cell in a dark `Frame` (`className="dark"`, `background:
  var(--background)`, `color: var(--foreground)`, `fontFamily: var(--font-sans)`,
  padding, rounded border). **This is required** — the preview card body is
  hardcoded white by the emitter, so `text-foreground` (near-white) is invisible
  without a dark surface.
- Use realistic CS2 content (maps: Mirage/Inferno/Nuke; mono stats K/D/A/MVP;
  lime accent on standout stats). Fixtures follow `lib/api/types.ts`
  (Match/Video/FeedItem/Play/DemoPlayer/Preset/Song).
- Overlays (Dialog/Sheet/DropdownMenu/Tooltip): set
  `cfg.overrides.<Name> = {"cardMode":"single","viewport":"WxH"}` and render the
  OPEN state inside the card; compose with SidebarProvider/TooltipProvider where
  the component needs context.

## Scope

51 components carded; 70 structural shadcn sub-parts excluded via
`componentSrcMap: null` (kept importable on the global, shown composed inside
their parent's preview). `window.FragForge` still exports all ~126 names.

## Per-component authoring learnings (folded from the fan-out waves)

- **Shims for context hooks**: `@/lib/session` is shimmed (returns a fixed signed-in
  session) so AppSidebar, LinkHistoryStep, PairPcStep render without a
  SessionProvider. Same pattern as `@/lib/api`. A component that calls a
  context hook with no provider throws — and the capture reports **0 errors**
  (the throw is swallowed into an empty cell). A blank/tiny PNG is the only
  signal — always READ the sheet, never grade an unread hook-dependent cell.
- **Overlays** (Dialog/Sheet/DropdownMenu/Tooltip/SongPickerDialog) are rendered
  controlled-`open` on a dark stage with `cfg.overrides.<Name> = {cardMode:
  single, viewport}`. Dialog/Sheet/SongPicker keep a `position:fixed` full-bleed
  stage (their radix scrim reads as an intentional modal backdrop). DropdownMenu
  and Tooltip have NO scrim, so use a **sized** dark block (width + minHeight),
  not `position:fixed` (which doesn't fill behind the portal).
- **Sidebar**: render the raw `Sidebar` primitive with `collapsible="none"` for a
  static inline column. `AppSidebar` (the product shell) needs `cardMode:single`
  (it portals a mobile Sheet that escapes a grid cell).
- **Images don't load offline** in the render check. Prefer a component's own
  no-image fallback: ReadyCard/RenderingCard use ReelCover when `thumbnailUrl`
  is omitted; Avatar uses AvatarFallback (force it via empty `<AvatarImage src="" />`);
  pass `authorAvatarUrl: ''` for initials. FeedCard has NO fallback — it renders
  a bare `<img>`; use an inline-SVG data URI (URL-encode `#` as `%23`, strip
  newlines) for a 9:16 placeholder.
- **Fixture/prop gotchas**: `Match.playedAt` is an ISO string; `FeedItem.createdAt`
  is epoch ms (`timeAgo` accepts both). `FailedCard.onChange` is required (not
  optional like ReadyCard). `PlayerPicker` expects players pre-sorted by kills
  (`players[0]` is auto-highlighted). `PresetCards` `value` is the preset `name`,
  not an index. `ToggleGroup` needs `type="single"|"multiple"` + matching
  `defaultValue`; `variant="outline"` is most legible on dark.
- **Layout**: row-layout cards (RenderingCard, FailedCard) need width ~420 (340
  truncates). `Separator orientation="vertical"` collapses to 0 without an
  explicit inline height in a flex row. `ScrollArea` needs an explicit height +
  overflowing content to read as scrollable. GrainOverlay (`position:fixed`,
  ~3.5% opacity) needs a `position:relative` host panel with `contain:paint`.
  Wordmark forwards only `className` (no `style`) — scale it with a `transform`
  wrapper. ReelCover is `size-full` (no intrinsic size) — give it an
  `aspect-ratio:9/16` fixed box.
- **Toaster**: the live sonner region is empty in static capture and `toast` is
  not re-exported through `cs2video-web`, so its card is carried by token-based
  static toast mockups (using `--popover`/`--popover-foreground`/`--border`).
- **Preview styling**: inline styles + CSS tokens (`var(--primary)` lime,
  `var(--font-mono)`, `var(--muted-foreground)`, etc.) only — novel Tailwind
  utility classes in preview JSX render UNSTYLED (not in the compiled sheet).

## Known render warns (triaged, not new)

- `[FONT_REMOTE]` Inter/JetBrains Mono/Space Grotesk — expected (Google Fonts
  @import). Not a missing-font failure.

## Re-sync risks (watch-list)

- The stub `node_modules/cs2video-web` and the compiled
  `node_modules/cs2video-web/styles.css` are gitignored — recreate the stub
  (above) and re-run `compile-css.mjs` on a fresh clone before building.
- MatchRow/feature fixtures are inlined in the preview .tsx; if `types.ts`
  field names change, the fixtures need updating (TypeScript won't catch it —
  previews compile loose).
- `.d.ts` prop bodies are empty (synth mode). If the agent contract quality
  matters, add `cfg.dtsPropsFor` or a real declaration build.
- Playwright 1.60.0 pins chromium 1223 (matches the local ms-playwright cache).
  A different machine may need a different playwright version (check
  `node_modules/playwright-core/browsers.json` against the cached build).
