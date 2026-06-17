# Spec — Harden the upload→reel path to "100% functional" (no dead-ends)

- Date: 2026-06-17
- Status: approved (scope + approach confirmed by user 2026-06-17)
- Branch: `harden/upload-reel`
- Builds on: `docs/specs/2026-06-16-upload-parser-real.md` (the real upload→reel pipeline)

## Context (verified state, 2026-06-17)

The full upload→reel pipeline is **demonstrably functional end-to-end** on a fresh
build (Go 1.26.3, on `main` after the `d59f6e4` authz hardening). Verified live this
session: upload a 403 MB pro demo → scan (~6s) → parse (~6s, 14 segments) → record
(real CS2/HLAE, ~4 min) → render `viral-60-clean` (~1.5 min) → download. `seg-001`
is a real 1080×1920 h264 reel, downloadable through the web proxy
(`/api/demos/{job}/renders/viral-60-clean/videos/seg-001`, 17.9 MB) plus a JPEG cover.

What remains for "100% functional" is the **robustness** of that one path — it has
dead-ends that make it fragile, not broken. This spec hardens those.

## Goal

A user at `localhost` can take the no-login upload→reel path and **always** end up
either with a finished, watchable/downloadable reel or a clear, recoverable error —
with no orphaned, vanished, or silently-stuck reels, including across a page reload.

## Scope

**In scope** (the real upload→reel path only):
- Surface the orchestrator's real failure reason to the UI.
- Make in-flight and finished reels survive a hard reload / direct visit.
- Make the record→render orchestration resumable and idempotent (reattaches after reload).
- Show failed reels with their reason and a working Retry.
- Stop fabricating `failed` on client poll-timeout.

**Out of scope** (stays mock, per user decision): Steam login, match history, PC
pairing, feed/publish backend, song preview audio. No backend pipeline changes — the
orchestrator already exposes everything needed. No new runtime dependencies.

## Root problem

`web/lib/api/real.ts` `RealApiClient` keeps the reel path's UI state — both in-flight
and finished reels — in a **process-memory `Map` (`reels`, line 61)** mutated by a
**fire-and-forget promise (`void orchestrateReel`, line 273)**. The orchestrator is
already the durable source of truth (`job.status`, `failure_reason`, render-variant
state), but the client never reconciles against it. Consequences:

1. **Reload orphans in-flight reels.** The `Map` is browser-process memory; a hard
   reload / direct visit to `/videos` starts empty and the driving promise is dead.
   The job keeps running server-side but the UI has lost its handle.
2. **Failures vanish.** `/videos` splits videos into `rendering`
   (`status !== 'ready' && status !== 'failed'`) and `ready` (`status === 'ready'`),
   so a `failed` reel matches neither section and disappears — no reason, no retry
   (`web/app/(app)/videos/page.tsx:77-78`).
3. **No failure reason reaches the UI.** `web/app/api/demos/[jobId]/status/route.ts`
   strips the upstream job JSON to `{ status }` (line 15-16), dropping
   `failure_reason`; `orchestrateReel`'s `catch { patch({ status: 'failed' }) }`
   (real.ts:202-204) records no reason either.
4. **Client timeouts fake a failure.** `waitForStatus`/`waitForRender` throw on poll
   exhaustion (12 min / 10 min), which `orchestrateReel` turns into `failed` even
   though the rig may still be capturing.

## Design principle

**The orchestrator is the source of truth.** The browser persists only lightweight
reel **intents** and always derives live status (and failure reason) by reconciling
each intent against the orchestrator. Driving the pipeline forward is **idempotent
against server state**, so a reload simply reattaches and continues.

### Reel intent (persisted)

Stored as a list in `localStorage` under `fragforge.reels.v1`, SSR-guarded and
quota-tolerant (mirroring the mock's `sessionStorage` pattern in `web/lib/api/mock.ts`):

```ts
type ReelIntent = {
  videoId: string;     // `${jobId}__${segmentId}`
  jobId: string;
  segmentId: string;
  mode: RenderMode;    // 'clean' | 'music'
  songId?: string;
  title: string;       // captured at createVideo time so cards render without refetch
  map: string;
  score: string;
  createdAt: number;
};
```

Intents are durable facts ("the user asked for this reel"). Status, downloadUrl, and
failureReason are **derived** each tick from the orchestrator — never persisted.

### Reconcile (pure derivation)

A pure, framework-free function maps server truth → UI state + the next action to
drive. This is the testable heart of the feature:

```ts
type ReelView = {
  status: 'queued' | 'recording' | 'composing' | 'ready' | 'failed';
  action: 'record' | 'render' | 'none';   // the single idempotent step to drive next
  downloadUrl?: string;
  thumbnailUrl?: string;
  failureReason?: string;
};

deriveReelView(jobStatus, renderStatus, ids) => ReelView
```

Rules (idempotent against server state — never double-drives):
- job `failed` → `{ status:'failed', failureReason, action:'none' }`
- render `ready` → `{ status:'ready', downloadUrl, thumbnailUrl, action:'none' }`
- render `failed` → `{ status:'failed', failureReason, action:'none' }`
- render `queued|rendering` → `{ status:'composing', action:'none' }`
- job `parsed` and no render yet → `{ status:'queued', action:'record' }`
- job `recording` → `{ status:'recording', action:'none' }`
- job `recorded|composed` and no render yet → `{ status:'composing', action:'render' }`
- otherwise → map job status to the nearest UI status, `action:'none'`

The client performs `action` at most once per tick and relies on server state (not a
local flag) to decide, so POST `/record` is only issued while `parsed` and POST
`/render` only before a render exists — safe to run every poll, before and after reload.

## Tracer-bullet slices

Each slice ends in a runnable, independently-verified state against the running stack
(`orchestrator :8080`, web `:3300`).

### TB-1 — Forward the failure reason through the status proxy
- **Files:** `web/app/api/demos/[jobId]/status/route.ts`
- **Change:** forward `failure_reason` alongside `status` when the upstream job carries
  it (only the two known fields — never the raw upstream object, per the proxy's
  existing security posture in `_lib.ts`).
- **Verify:** `npm run typecheck` + `npm run lint`; live smoke that a non-failed job's
  `/status` still returns `{ status }` (no spurious key). The failed-job display is
  exercised end-to-end in TB-4.

### TB-2 — Durable reel intents + rehydrate on load
- **Files:** new `web/lib/api/reel-store.ts` (localStorage load/save, SSR/quota-guarded);
  `web/lib/api/real.ts` (write an intent in `createVideo`; rehydrate intents in the
  constructor / `listVideos`).
- **Change:** persist a `ReelIntent` on `createVideo`; on load, rebuild the reel list
  from stored intents (status shown from last-known/derived, see TB-3).
- **Verify:** create a reel, hard-reload `/videos` → the reel is still listed.

### TB-3 — Resumable, idempotent reconcile loop (pure core)
- **Files:** new `web/lib/api/reel-reconcile.ts` (pure `deriveReelView`) +
  `web/lib/api/reel-reconcile.test.ts` (Node built-in `node:test`); `web/lib/api/real.ts`
  (replace fire-and-forget `orchestrateReel` with a per-tick reconcile in `listVideos`
  that fetches job + render status, calls `deriveReelView`, and performs the single
  `action`).
- **Change:** `listVideos` (polled every 1.5s by `/videos`) becomes the reconcile
  driver. No long-lived promise; each tick reattaches to whatever the server says.
- **Verify:** `node --test` on the pure core (RED→GREEN); live: start a reel, hard-reload
  during `recording` → it advances to `ready` unattended.

### TB-4 — Failed reels stay visible, with reason + Retry
- **Files:** `web/app/(app)/videos/page.tsx` (a Failed section; keep `failed` reels);
  `web/components/videos/rendering-card.tsx` or a new `failed-card.tsx` (reason + Retry);
  `web/lib/api/real.ts` (`retryVideo(id)` re-drives the failed stage);
  `web/lib/api/client.ts` + `web/lib/api/mock.ts` (interface parity).
- **Change:** render `failed` reels with their `failureReason` and a Retry button that
  re-issues the appropriate stage (record is not auto-retried server-side — user decides,
  matching `docs/architecture/02-data-flow.md`).
- **Verify:** force a job failure (throwaway orchestrator with a stub recorder that exits
  non-zero — no CS2) → the card shows the reason; Retry recovers on the real stack.

### TB-5 — Client poll-timeout ≠ failure
- **Files:** `web/lib/api/real.ts` (remove the fabricated-`failed` on poll exhaustion).
- **Change:** with reconcile (TB-3) there is no fixed client deadline; the card keeps
  showing `recording`/`composing` (with elapsed time) until the server reports a real
  terminal state. Only orchestrator `failed` is failure.
- **Verify:** a long-running record never flips the card to `failed` on its own.

## Testing strategy

- **Pure logic** (`deriveReelView`, reel-store serialization) → `node:test` +
  `node:assert`, run with `node --test` (built into Node 24; **no new dependency**).
  Framework-free modules with no `next/*` imports so they run standalone.
- **Route handlers + React components** (thin wiring) → `npm run typecheck`,
  `npm run lint`, and live verification against the running stack (curl + the browser via
  chrome-devtools). This matches repo convention — the existing proxy routes and pages
  have no unit tests, and the orchestrator's `failure_reason` emission is already covered
  by Go `handlers_test.go`.
- **Per-slice live verification** is mandatory: each TB is confirmed working in the
  running `:3300`/`:8080` stack before moving on. No "compiles == works".

## Risks / notes

- `RenderMode` value names and `Video`/`Match` field names are taken from
  `web/lib/api/types.ts`; confirm before editing.
- Reconcile must remain strictly idempotent against server state; the pure-core tests
  must cover "already recording", "already rendering", and "render ready" to prevent
  double-driving (double POST `/record`/`/render`).
- localStorage is per-origin and unbounded over time; intents are small, but
  `createVideo` should cap/prune (e.g. keep the most recent N) to avoid unbounded growth.
- No orchestrator/Go changes; if a backend gap surfaces, stop and re-scope.
