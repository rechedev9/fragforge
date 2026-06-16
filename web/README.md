# CS2.VIDEO — web (Fase 1)

The CS2.VIDEO frontend: turn a CS2 player's matches into short highlight videos.

This is a standalone Next.js 15 app (App Router, React 19, TypeScript, Tailwind
CSS v4) built on **[shadcn/ui](https://ui.shadcn.com)**. It lives inside the
FragForge Go monorepo under `web/` but is fully independent — it does not import
or build any Go code.

## Design

The look & feel is the **v2 "replay studio"** identity: a left-sidebar shell,
acid-lime signal color on charcoal, and monospace tabular numbers for every
stat. The **design contract** lives in [`web/design.md`](./design.md) — read it
before changing anything visual. It defines the palette (oklch tokens in
`app/globals.css`), the fonts (Space Grotesk / Inter / JetBrains Mono), the
sidebar IA, and the signature components (`PipelineSteps`, `ScoreBar`,
`StatMono`, `RecDot`, `Filmstrip`, `GrainOverlay`).

UI primitives come from shadcn/ui (`components/ui/*`, configured in
`components.json` with the `neutral` base color and lime token overrides).
Brand-specific pieces live in `components/brand/*`.

## Status: mock API only

Fase 1 is the **frontend only**, running against a typed **in-memory mock API**
(`lib/api/mock.ts`). There is no real backend yet:

- Sign in, match history, PC pairing, matches, clips, songs, videos, and feed
  are all served from `lib/api/fixtures.ts`.
- Created videos advance `queued → recording → composing → ready` based on
  elapsed time, so the UI can poll and show real-looking progress with no timers.

The real backend wires in during **Fase 2**: implement a `RealApiClient` against
the same `ApiClient` interface (`lib/api/client.ts`) and select it in
`lib/api/index.ts` when `NEXT_PUBLIC_API_BASE` is set. No screen code changes.

## Run locally

Requires Node 20+.

```bash
cd web
npm install
npm run dev
```

Open http://localhost:3000.

Other scripts:

```bash
npm run build      # production build
npm run start      # serve the production build
npm run lint       # next lint
npm run typecheck  # tsc --noEmit
```

## Deploy to Vercel

1. Import the repository into Vercel.
2. Set the project's **Root Directory** to `web`.
3. Framework preset: **Next.js** (auto-detected). No env vars are needed for the
   mock phase.
4. Deploy.

## Layout

```
web/
  app/                 # routes (App Router)
    layout.tsx         # root layout: fonts, <html class="dark">, GrainOverlay, Toaster
    page.tsx           # / — Steam auth (no sidebar)
    connect/page.tsx   # /connect — link history + pair PC (no sidebar)
    (app)/layout.tsx   # authenticated shell (left Sidebar + container)
    (app)/matches/     # /matches and /matches/[id]
    (app)/videos/      # /videos (Library)
    (app)/feed/        # /feed
  components/
    ui/                # shadcn/ui primitives (button, card, sidebar, dialog, ...)
    brand/             # PipelineSteps, ScoreBar, StatMono, RecDot, Filmstrip, GrainOverlay, Wordmark
    shell/             # app-sidebar (left nav + slots meter + user menu)
    matches/ clips/ videos/ feed/ login/ connect/   # per-screen pieces
  lib/
    api/               # types, ApiClient interface, MockApiClient, fixtures
    session.tsx        # SessionProvider + useSession
    format.ts          # cn(), timeAgo, formatCountdown, productStatusLabel, ...
  design.md            # v2 design contract (read this before visual changes)
```
