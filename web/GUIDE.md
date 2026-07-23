# FragForge Web UI Guide

`web/` is the Next.js UI shipped inside the FragForge Windows desktop app. The
desktop process starts this app as a standalone Next.js server alongside the
local Go orchestrator, then opens it in an Electron window. It is not a hosted
web application.

The production upload flow uses `RealApiClient`. Browser requests go to the
same-origin `/api/demos/*` route handlers, which proxy the local orchestrator
server-side:

```text
Electron renderer
  -> Next.js /api/demos/*
  -> local zv-orchestrator
  -> parse, HLAE/CS2 capture, render, and local artifacts
```

The browser never receives the orchestrator URL or token. The orchestrator is
the source of truth for jobs and artifacts; the client stores only lightweight
reel intent in `localStorage`. Screens without an orchestrator-backed product
surface still use typed fixture data through `MockApiClient`.

## Local mutation capability

Read-only proxy requests require the loopback/Origin guard. Every `POST`,
`PUT`, `PATCH`, and `DELETE` additionally requires the HttpOnly,
`SameSite=Strict`, `Path=/` cookie named `fragforge_proxy_capability`. The
Next server compares it in constant time with its server-only
`FRAGFORGE_PROXY_MUTATION_CAPABILITY` environment value and fails closed when
either is absent.

Electron main owns this integration: generate a fresh high-entropy value for
each launch, pass it only to the Next child as that environment variable, and
seed the cookie through Electron's `session.cookies` before `loadURL`. Never
place the value in `NEXT_PUBLIC_*`, HTML, a route response, or renderer
JavaScript. The value is a proxy capability, separate from
`ORCHESTRATOR_TOKEN`, which remains server-side.

For `scripts/local-studio.ps1`, a second server-only
`FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY` is generated (or accepted from the
environment) and printed only to the launching terminal. Open `/bootstrap` and
enter it in the password form. The same-origin POST validates that bootstrap
value, then sets the separate mutation capability as the HttpOnly cookie. This
route is disabled when either server-side value is absent; it never places a
capability in a URL or renderer JavaScript.

## Design

The look and feel is the v2 "replay studio" identity: a left-sidebar shell,
acid-lime signal color on charcoal, and monospace tabular numbers for every
stat. The design contract lives in [`design.md`](./design.md). Read it before
changing anything visual; it defines the palette, fonts, sidebar information
architecture, and signature components.

UI primitives come from shadcn/ui (`components/ui/*`, configured in
`components.json`). Brand-specific pieces live in `components/brand/*`.

## Run locally

The supported development launcher starts the SQLite-backed orchestrator and
the web UI together, then opens the upload flow:

```powershell
# From the repository root, after .\scripts\build.ps1
.\scripts\local-studio.ps1
```

For frontend-only work, start a local orchestrator separately and then run:

```powershell
cd web
pnpm install
pnpm run dev
```

Open `http://localhost:3000`. `ORCHESTRATOR_URL` is a server-side setting and
defaults to `http://127.0.0.1:8080`.

Verification commands:

```powershell
pnpm run typecheck
pnpm run lint
pnpm run test:unit
pnpm run build
```

## Desktop packaging

`output: 'standalone'` in `next.config.mjs` produces the self-contained server
bundle assembled into the Windows installer. See
[`../desktop/GUIDE.md`](../desktop/GUIDE.md) for the installer build and boot
architecture.

## Layout

```text
web/
  app/                         # App Router pages and same-origin API routes
    api/demos/                 # server-side proxy to the local orchestrator
    upload/page.tsx            # no-login demo upload flow
    (app)/matches/             # match and clip selection views
    (app)/videos/              # local reel library
    (app)/feed/                # feed view
  components/
    ui/                        # shadcn/ui primitives
    brand/                     # FragForge presentation components
    shell/                     # app shell and capture readiness
    matches/ clips/ videos/    # feature components
  lib/
    api/                       # typed clients, contracts, stores, and fixtures
    format.ts                  # shared display formatting
  design.md                    # v2 visual design contract
```
