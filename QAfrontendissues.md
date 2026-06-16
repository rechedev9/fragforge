# Frontend QA — issues log

Manual QA sweep of the FragForge web app via chrome-devtools.

- **Date:** 2026-06-16
- **Build/env:** dev server `npm run dev` on `http://localhost:3300`, `NEXT_PUBLIC_API_BASE=/api` (RealApiClient → local orchestrator for the upload/reel flow; everything else delegates to the in-memory mock).
- **Scope:** landing, connect, matches, match detail, upload, library/videos, feed, navigation/shell, 404, mobile. Interactions (clicks, forms, filters, toggles) + console were checked per screen.
- **Severity:** **High** (broken core action / dead CTA) · **Medium** (broken/missing feature, off-brand) · **Low** (UX/label) · **Nit** (polish).

> Most flows actually work (see "Verified working" at the bottom). The items below are what's broken or off.

---

## Resolution — all fixed (2026-06-16)

| # | Status | Fix |
| --- | --- | --- |
| QA-1 | ✅ Fixed | "Get more" now fires a sonner toast ("More render slots are coming soon…") — `components/shell/app-sidebar.tsx`. Verified: toast renders on click. |
| QA-2 | ✅ Fixed | Mock "ready"/seed reels now point at a **same-origin** sample (`/sample-reel.mp4`, generated into `web/public` by `run-local.sh`) instead of a dead `example.com` URL — `lib/api/fixtures.ts` `SAMPLE_REEL_URL`, `lib/api/mock.ts`. Verified: View plays (readyState 4). |
| QA-3 | ✅ Fixed | Feed cards are now click-to-play (inline `<video>` dialog) instead of a decorative ▶ — `components/feed/feed-card.tsx`. Verified: play opens + loads the sample. |
| QA-4 | ✅ Fixed | Added a branded `web/app/not-found.tsx` ("This page got fragged." + Back to matches / Home). Verified. |
| QA-5 | ✅ Fixed | Mock session persists to `sessionStorage` (survives reload) and the sidebar shows a **Sign in** link when signed out — `lib/api/mock.ts`, `components/shell/app-sidebar.tsx`. Verified: reload keeps the user; signed-out shows Sign in. |
| QA-6 | ✅ Fixed | Connect shows the "N matches found" badge ~1.2s before advancing — `components/connect/link-history-step.tsx`. |
| QA-7 | ✅ Fixed | "Best frags first" now sorts by **kills** (K/D only breaks ties) — `app/(app)/matches/page.tsx`. Verified order. |
| QA-8 | ✅ Fixed | Download uses an `<a download>`; Share uses the Web Share API, falling back to clipboard-copy + toast — `components/videos/ready-card.tsx`. |
| QA-9 | ✅ Fixed | Feed cards render the real thumbnail image (`<img>`) instead of a gradient placeholder — `components/feed/feed-card.tsx`. Verified: thumbnails load. (Library `ReadyCard` already shows real covers.) |
| QA-10 | ❎ Not a bug | The "Find highlights" label is present in the markup with no responsive hiding — the earlier "arrow-only" view was the mobile sheet overlay cropping the row. No change needed. |

---

## Issues

### QA-1 — "Get more" (slots meter) is a dead button — **High**
- **Where:** sidebar footer, on every `(app)` page (`/matches`, `/matches/[id]`, `/videos`, `/feed`) + the mobile sheet.
- **Steps:** click **Get more** next to the SLOTS meter.
- **Expected:** opens an upgrade/slots dialog or navigates somewhere.
- **Actual:** nothing happens — no navigation, no dialog, no toast. The CTA is a no-op.
- **Likely fix:** wire an action (modal or route) or remove the button until there's a destination.

### QA-2 — Library "ready" reels can't be viewed/downloaded — **Medium**
- **Where:** `/videos` (Library), the seeded "ready" cards (`5K - Clean POV`, `4K - Music Edit`).
- **Steps:** hover a ready card → click **View** (also **Download** / **Share**).
- **Expected:** the reel plays / downloads.
- **Actual:** **View** opens the inline player but it shows *"No se ha podido reproducir el contenido multimedia"* — the seed `downloadUrl` is a placeholder (`https://example.com/mock/...mp4`, 404). **Download** and **Share** just `window.open()` that same dead URL (opens a 404 tab).
- **Note:** reels produced by the real upload→record→render pipeline DO play in the inline player; this is the seeded/mock data. Still presents as broken in the UI.
- **Likely fix:** give seed videos a real sample URL (or hide actions until a real `downloadUrl` exists).

### QA-3 — Feed reels are not playable — **Medium**
- **Where:** `/feed`.
- **Steps:** hover a reel card (a ▶ play overlay appears) → try to click it / the card.
- **Expected:** the community reel opens/plays.
- **Actual:** the ▶ overlay is decorative — there is no clickable play/view action in the card (only the **Like** button is interactive). There's no way to actually watch a feed reel.
- **Likely fix:** make the card/▶ open a player (the feed fixtures already carry `videoUrl`).

### QA-4 — No custom 404 page — **Medium**
- **Where:** any unknown route, e.g. `/this-route-does-not-exist`.
- **Expected:** branded 404 within the shell, with a link back home.
- **Actual:** Next.js default, unstyled *"404 — This page could not be found."* — no sidebar, no FragForge styling, no navigation back.
- **Likely fix:** add `web/app/not-found.tsx` (and/or route-group not-found) styled to the app with a "back to matches/home" link.

### QA-5 — Session is lost on refresh; no auth guard / no sign-in affordance when signed out — **Medium**
- **Steps:** sign in with Steam, then hard-refresh any `(app)` page (or open one directly in a new tab).
- **Expected:** stay signed in, or be sent to sign-in.
- **Actual:** the mock auth session is in-memory only, so a full reload resets it → the sidebar **user widget (avatar + name + sign-out) disappears**, yet `(app)` routes still render fully and the sidebar offers **no way to sign in** to recover. (Uploaded demos persist to `sessionStorage`; the auth session does not.)
- **Likely fix:** persist the session (sessionStorage, like uploads) and/or render a "Sign in" entry in the sidebar footer when `session.user` is null; decide whether `(app)` routes should redirect to `/` when unauthenticated.

### QA-6 — Connect: "N matches found" confirmation never shown — **Low**
- **Where:** `/connect`, step 1 (Link your match history).
- **Steps:** fill auth code + sharecode → **Load my matches**.
- **Actual:** it jumps straight to step 2 (Pair your PC); the "matches found" feedback the step is meant to show isn't visible (advances before the badge renders).
- **Likely fix:** show the matches-found count briefly (or a confirmation) before/while advancing.

### QA-7 — "Best frags first" sorts by K/D, not kills — **Low**
- **Where:** `/matches` filter `Best frags first`.
- **Actual:** sorts by **K/D ratio** descending (Inferno 18K/2.38 ranks above Anubis 23K/1.64). "Frags" usually means kill count, so the order can surprise.
- **Likely fix:** sort by kills (or relabel, e.g. "Best K/D first").

### QA-8 — Download / Share are not real actions — **Low**
- **Where:** `/videos` ready cards.
- **Actual:** both **Download** and **Share** call `window.open(downloadUrl)`. Download opens the file in a tab instead of downloading (no `download` attribute / Content-Disposition), and Share has no share-sheet / copy-link behavior — it's the same as View/Download.
- **Likely fix:** Download via an `<a download>` (or a proxy that sets `Content-Disposition`); Share should copy a link or open a share dialog.

### QA-9 — Library/Feed card covers are gradient placeholders, not the seeded thumbnails — **Low**
- **Where:** `/feed` and `/videos` cards.
- **Actual:** cards render a generated gradient/grain cover rather than the seeded `thumbnailUrl` (picsum) images. Verify this is intentional (branded cover) vs. broken image loading.

### QA-10 — Mobile: "Find highlights" button shows only an arrow — **Nit**
- **Where:** `/matches` at mobile width (≤~400px).
- **Actual:** the "Find highlights" CTA collapses to an arrow-only icon (label hidden; accessible name is still "Find highlights"). Verify this is intended responsive behavior vs. unwanted truncation.

---

## Verified working (no issues found)

- Landing `/`: hero, **Continue with Steam** (→ `/connect`), **Upload a demo** (→ `/upload`).
- `/connect`: link-history form (enable/disable, submit), **"How to get these"** now points to the correct CS2 wizard, pair-PC (generate code, copyable, paired badge), **Enter the studio** → `/matches`.
- `/matches`: list renders, **All / Wins only** filter, **map search**, **Best frags** reorders (see QA-7), **Find highlights** links.
- `/matches/[id]`: summary + stats, filmstrip play selection, **Clean POV / Music Edit** mode cards, song picker dialog, **Create reel** → `/videos`.
- `/videos`: pipeline stepper (Queued→Capturing→Editing→Ready), RecDot "LIVE ON YOUR RIG", **Publish** toggle, inline player (works for real pipeline reels).
- `/feed`: **Like** toggle (count increments, turns lime).
- `/upload`: dropzone rejects non-`.dem` ("That file is not a .dem demo.").
- Shell: sidebar active states, user dropdown → **Sign out**, mobile sidebar → **Sheet** with full nav.
- **Console:** no errors/warnings observed on any screen during the sweep.
