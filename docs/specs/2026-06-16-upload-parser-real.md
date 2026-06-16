# Spec: Upload → parser real (Flow B, real .dem parsing)

Status: approved design, ready to implement.
Date: 2026-06-16.
Owner: web upload flow + orchestrator parse path.

## Goal

Make the **Upload a demo** flow (Flow B) parse a real `.dem` against the existing
Go pipeline instead of synthesizing fake matches. The slice ends at **real
highlights on screen** in `/matches/[id]`. Recording/rendering ("Create reel")
stays mocked — real capture needs HLAE/CS2 + a paired PC, which is out of scope.

Two product decisions are already made:

1. **Target player = picker after a roster scan.** The parser is target-centric
   (needs a SteamID64 before it segments). After upload we run a lightweight
   roster scan, show the player list with K/D/A, and the user picks who to clip.
2. **Parsing runs on the user's own machine** via the orchestrator in memory
   mode (`ZV_DATABASE_URL=memory` → in-memory repo + inline queue, no
   Postgres/Redis/HLAE). The `.dem` never leaves the machine, so the
   "the .dem never leaves your machine" copy stays true. Local-first only; no
   deploy.

## Topology (local-first)

```
Browser (Next dev server)
  → Next.js route handlers under web/app/api/demos/*   (server-side fetch; keeps CORS + token server-side)
    → Orchestrator http://127.0.0.1:8080 (ZV_DATABASE_URL=memory)
      → scan:roster / parse:demo (inline worker) → killplan.Plan
```

The browser only talks to same-origin Next route handlers. The route handlers
read a server-side `ORCHESTRATOR_URL` (default `http://127.0.0.1:8080`) and
optional `ORCHESTRATOR_TOKEN` (sent as `X-FragForge-Token`). `NEXT_PUBLIC_API_BASE`
presence is the toggle that selects the real client (see Web §).

## The data reality (mapping killplan → UI)

The parser produces highlight **segments for one player**, not a full scoreboard.

| UI field | Source | Plan |
| --- | --- | --- |
| `Play` (round, kills, weapon) | `killplan.Segment` + `Segment.Kills` | map 1:1 |
| `Match.map` | `Plan.Demo.Map` (`de_inferno`) | prettify → `Inferno` |
| `Match.stats.kills` | `Plan.Stats.TotalKillsTarget` (or roster) | direct |
| `Match.stats.deaths/assists` | **roster scan tally** for chosen player | from roster |
| `Match.stats.kd` | kills/deaths (guard div-by-zero) | compute |
| `Match.stats.mvps` | not available | `0` |
| `Match.score` | not available (parser computes no round score) | `""` (UI hides empty) |
| `Play.thumbnailUrl` | no video yet | placeholder (picsum seed) |

The roster scan tallies per-player **kills, deaths, assists** in one pass so it
feeds both the picker and the `Match` stats.

---

# Backend (Go) — owned by the Go implementation agent

All new Go must compile and pass `go build ./... && go vet ./...`, targeted
tests, and the repo gate (`scripts/go-gate.sh --no-format`). Add tests. Match
existing patterns in each file; do not introduce new abstractions. Read the
neighboring code before editing. Error strings lowercase, no trailing
punctuation, wrap with `%w` where callers unwrap.

## 1. `internal/parser` — roster scan (new file `roster.go`)

Add a single-pass roster scan that reuses the existing event-handling style in
`demo_kills.go` (`p.RegisterEventHandler(func(e events.Kill){...})`,
`p.GameState()`, helpers `teamLabel`, `parseToEnd`).

```go
// PlayerStat is one player's tally from a roster scan of a demo.
type PlayerStat struct {
    SteamID64 string `json:"steamid64"`
    Name      string `json:"name"`
    Team      string `json:"team"` // "CT" | "T" | "" (last observed)
    Kills     int    `json:"kills"`
    Deaths    int    `json:"deaths"`
    Assists   int    `json:"assists"`
}

// Roster does one pass over the demo and returns every human player it saw,
// with kill/death/assist tallies, sorted by Kills desc then Name asc.
func Roster(p demoinfocs.Parser) ([]PlayerStat, error)

// RosterWithContext is the cancellation-aware variant, mirroring
// RunWithContext (watcher goroutine that calls p.Cancel() on ctx.Done(),
// joined before return; returns the context error on cancellation).
func RosterWithContext(ctx context.Context, p demoinfocs.Parser) ([]PlayerStat, error)
```

Implementation notes:
- On each `events.Kill`: increment killer's `Kills`, victim's `Deaths`, and (if
  present) `e.Assister`'s `Assists`. Capture/refresh `Name` and `Team`
  (`teamLabel`) for every involved player. **Verify the assist field name**
  against demoinfocs v5 (`events.Kill.Assister *common.Player`); adapt if it
  differs.
- Identify players by `SteamID64` (uint64 → decimal string). **Skip bots /
  zero SteamID** (`SteamID64 == 0`).
- Tally into a `map[uint64]*PlayerStat`; emit sorted slice.
- Do not require a target; never returns `ErrTargetNotFound`. An empty demo
  yields an empty slice (not an error).
- Factor the `RunWithContext` watcher into a shared helper if convenient, or
  duplicate the small pattern — keep it readable.

Tests (`roster_test.go`): table test using the same fixture/test-demo mechanism
the existing parser tests use (`TEST_DEMO_PATH` or `testdata/*.dem`). If the
test demo is unavailable, skip with `t.Skip` like the existing parser tests do
(check how `demo_test.go` / `app_test.go` gate on the demo). Assert the roster
is non-empty, sorted by kills desc, contains expected steamids, and skips bots.

## 2. `internal/job` — new statuses (append, do not renumber)

Append to the `Status` iota **after `StatusFailed`** so existing integer values
are unchanged:

```go
StatusScanning // queued→scanning while the roster scan runs
StatusScanned  // roster ready; awaiting the user's target pick
```

Add `"scanning"` and `"scanned"` to `statusNames` at the matching indices.
(`ParseStatus`/`String` are array-driven, so this is the only change.)

## 3. `internal/artifacts` — roster key

Add a key builder consistent with the existing ones (e.g. `RecordingResultKey`):

```go
// RosterKey is the storage key for a job's roster scan result.
func RosterKey(id uuid.UUID) string { return fmt.Sprintf("jobs/%s/roster.json", id) }
```

If `internal/artifacts/keys.go` is not where keys live, follow the actual
location/pattern used by `moments.ArtifactKey` and the recording keys.

## 4. `internal/tasks` — scan task

```go
const TypeScanRoster = "scan:roster"

type ScanRosterPayload struct { JobID uuid.UUID `json:"job_id"` }

// NewScanRosterTask mirrors NewParseDemoTask: same timeout (parseDemoTimeout)
// and a small max-retry (reuse parseDemoMaxRetry). Scanning is deterministic.
func NewScanRosterTask(id uuid.UUID) (*asynq.Task, error)
```

## 5. `internal/workers` — roster handler

Add `HandleScanRoster` to `ParserWorker` (it already has `repo` + `storage`).
Mirror `HandleParseDemo`/`parse`:

- Decode `tasks.ScanRosterPayload`; `repo.GetMeta`.
- `UpdateStatus(StatusScanning, "")`; log transition.
- Open demo from storage → copy to a temp `*.dem` (demoinfocs needs an
  `io.ReadSeeker`) → `demoinfocs.NewParser(tmp)` → `parser.RosterWithContext(ctx, p)`.
- Marshal roster (`json.MarshalIndent`) → `storage.Put(artifacts.RosterKey(j.ID), ...)`.
- `UpdateStatus(StatusScanned, "")`; log artifacts.
- On error: `recordTaskFailure(...)` like the parse path.

Register in `cmd/zv-orchestrator/main.go` next to the parse handler:
`taskHandlers[tasks.TypeScanRoster] = parserWorker.HandleScanRoster` (no new
worker/env gating — the parser worker is always enabled).

## 6. `internal/httpapi` — endpoints

### `CreateJob` (modify) — make `target_steamid` optional

In `handlers.go` `CreateJob`:
- Drop the hard `target_steamid is required` rejection. If present, still
  validate it's a uint64 and keep the **current behavior** (set
  `TargetSteamID`, create job, enqueue `NewParseDemoTask`).
- If **absent**: create the job with `TargetSteamID` empty, then enqueue
  `NewScanRosterTask(j.ID)` instead. Same demo storage + hashing. Same
  enqueue-failure compensation (mark `StatusFailed`).
- Response unchanged: `{ id, status }`.

### `GetRoster` (new) — `GET /api/jobs/{id}/roster`

- Read the job; if not found → 404.
- Open `artifacts.RosterKey(id)` from storage. If missing → 409
  `{ "error": "roster not ready" }` (still scanning, or this is a
  target-provided job with no scan).
- Stream/return the stored roster JSON shaped as:
  ```json
  { "players": [ { "steamid64": "765...", "name": "kekO", "team": "CT", "kills": 24, "deaths": 14, "assists": 5 } ] }
  ```
  (Store it already wrapped in `{ "players": [...] }` from the worker, or wrap
  on read — pick one and be consistent.) Read endpoint, no token.

### `StartParse` (new) — `POST /api/jobs/{id}/parse`

- Body JSON: `{ "target_steamid": "765...", "rules": { ...optional } }`.
- Validate `target_steamid` is a uint64 (reuse the same check as CreateJob).
- Allowed when job status is `StatusScanned` (or `StatusParsed` for re-parse);
  otherwise 409 like `StartRecording` does its state check.
- Persist `target_steamid` (and rules if provided, validated) on the job. There
  is no `repo.SetTarget`; add a minimal repo method **or** reuse an existing
  update path. Simplest: add `SetParseInputs(ctx, id, steamID string, r rules.Rules) error`
  to the repo interface used by handlers and implement it in both the Postgres
  repo and the in-memory repo. Follow how `SetKillPlan` is implemented in both.
- Enqueue `NewParseDemoTask(id)`; on enqueue failure, mark failed like CreateJob.
- Response: `{ id, status: "parsing" }` (or the job's current status).

### Routes (`routes.go`)

```go
r.Get("/api/jobs/{id}/roster", h.GetRoster)
r.Post("/api/jobs/{id}/parse", h.StartParse)
```

(`requireMutationToken` already covers the POST.)

### Handler tests (`handlers_test.go`)

Extend the existing table/handler tests: CreateJob without `target_steamid`
enqueues a scan and returns 201; CreateJob with it still enqueues a parse;
GetRoster returns 409 before scan and the player list after; StartParse rejects
a non-uint64 and a wrong-state job, accepts a scanned job. Use the existing test
harness (fake repo/queue/storage) — read `handlers_test.go` first and match it.

## 7. Postgres migration (create file, DO NOT run)

If the `job_status` enum is a Postgres enum type (see `migrations/00*_*.sql`),
add a migration `migrations/00X_job_status_scan.up.sql` (+ `.down.sql`) that
`ALTER TYPE job_status ADD VALUE 'scanning'; ... 'scanned';` and adds any
`target_steamid`-nullable handling needed. The MVP runs in **memory mode**, so
this is not executed — just keep the schema honest for the Postgres path. If the
status column is already plain text, no migration is needed; say so.

---

# Web (Next.js) — owned by the web implementation agent

Stack: Next 15 App Router, React 19, TS strict, Tailwind v4, shadcn/ui. Match
the "replay studio" design in `web/design.md` (acid-lime-on-charcoal, mono
tabular-nums for stats, Space Grotesk headings). All new code must pass
`cd web && npm run typecheck && npm run build`. Keep the existing mock working.

Contract changes are **additive** (per `web/design.md` §7): never change/remove
existing `ApiClient` fields or signatures.

## 1. `web/lib/api/types.ts` (additive)

```ts
export type DemoPlayer = {
  steamId: string; name: string; team: 'CT' | 'T' | '';
  kills: number; deaths: number; assists: number;
};
```

(Keep `Match.source?: 'steam' | 'upload'` as-is.)

## 2. `web/lib/api/client.ts` (additive methods)

```ts
scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[] }>;
parseDemo(input: { jobId: string; steamId: string }): Promise<Match>;
```

Keep `uploadDemo` in the interface (mark `@deprecated` in a doc comment —
superseded by scanDemo + parseDemo). Both mock and real implement the new
methods.

## 3. `web/lib/api/mock.ts` (implement new methods, keep deprecated one)

- `scanDemo(file)`: `await delay()`, mint a fake `jobId` (`m-upload-<seq>`),
  return ~10 deterministic `DemoPlayer`s (seed off `file.name`) with plausible
  K/D/A, sorted by kills desc. Persist a pending entry so `parseDemo` can resolve
  it. Reuse/extend the existing sessionStorage persistence.
- `parseDemo({ jobId, steamId })`: `await delay()`, synthesize a `Match` for the
  chosen player (reuse `synthUploadedMatch`, seed off `steamId`), store its plays,
  `source: 'upload'`, return the Match. Keep `getMatch`/`findClips`/`createVideo`
  resolving uploaded matches first (already do).

## 4. `web/lib/api/real.ts` (new) — RealApiClient

`class RealApiClient implements ApiClient`. **Wrap a `MockApiClient` fallback**
and override only the upload-real methods; delegate everything else (steam,
listMatches, listVideos, createVideo, feed, etc.) to the fallback so the rest of
the app keeps working in this slice.

- `scanDemo(file)`: POST the File as multipart to `/api/demos/scan`; the route
  returns `{ jobId }`. Then poll `/api/demos/{jobId}/status` until status is
  `scanned` (or `failed` → throw). Then GET `/api/demos/{jobId}/roster` →
  `{ players }`. Return `{ jobId, players }`. Map server `steamid64` → `steamId`.
- `parseDemo({ jobId, steamId })`: POST `/api/demos/{jobId}/parse` `{ steamId }`;
  poll status until `parsed` (or `failed` → throw); GET `/api/demos/{jobId}/plan`
  and `/api/demos/{jobId}/roster`; **map** killplan + roster → `Match`
  (see mapping below). Return Match.
- `getMatch(id)`: if `id` looks like a backend job id (UUID) → GET
  `/api/demos/{id}` (+ plan) and map to Match; else delegate to fallback.
- `findClips(matchId)`: if UUID → map plan segments → `Play[]`; else delegate.
- Polling: small helper with `await delay(800)` between tries, a max-attempts
  cap (e.g. 240 → ~3 min, parses are ~30–60s), and surfaces `failed` as an Error.

### Mapping helpers (in `real.ts` or `web/lib/api/map.ts`)

Define a minimal TS type mirroring the killplan JSON you consume
(`schema_version`, `demo.map`, `stats.total_kills_target`, `segments[]` with
`id`, `round`, `kills[]` each having `weapon`). Then:

- `prettifyMap('de_inferno') → 'Inferno'` (strip `de_`/`cs_`, title-case; fallback to raw).
- killplan + roster(for chosen steamId) → `Match`:
  `map` prettified; `score: ''`; `stats` from roster row (kills/deaths/assists,
  `kd = deaths ? +(kills/deaths).toFixed(2) : kills`, `mvps: 0`); `decentPlays =
  segments.length`; `source: 'upload'`; `thumbnailUrl` = picsum seed off jobId.
- segment → `Play`: `id = segment.id`, `matchId = jobId`, `round`,
  `kills = segment.kills.length`, `weapon` = most-frequent weapon in the segment
  (prettified, e.g. `ak47` → `AK-47` via a small lookup; fallback to raw),
  `kind: 'highlight'`, `label = `${kills}K · Round ${round}``, `thumbnailUrl`
  picsum seed off segment id.

## 5. `web/lib/api/index.ts` (toggle)

```ts
export const api: ApiClient = process.env.NEXT_PUBLIC_API_BASE
  ? new RealApiClient()
  : new MockApiClient();
```

Keep the Fase 1/Fase 2 comment. Default (no env) stays mock.

## 6. Next.js route handlers (new) — thin proxies to the orchestrator

Server-only. Read `process.env.ORCHESTRATOR_URL` (default
`http://127.0.0.1:8080`) and `process.env.ORCHESTRATOR_TOKEN` (optional →
`X-FragForge-Token`). Use `export const runtime = 'nodejs'`.

- `web/app/api/demos/scan/route.ts` — `POST`: read the incoming `FormData`
  (`file`), build a new `FormData` with field name **`demo`** and a `config`
  field omitted (no target → triggers the scan path), POST to
  `${ORCHESTRATOR_URL}/api/jobs`, return `{ jobId }` from the orchestrator's
  `{ id }`.
- `web/app/api/demos/[jobId]/status/route.ts` — `GET`: proxy
  `GET /api/jobs/{id}` → return `{ status }`.
- `web/app/api/demos/[jobId]/roster/route.ts` — `GET`: proxy
  `GET /api/jobs/{id}/roster` → return `{ players }` (map `steamid64`→`steamId`
  here or in the client — be consistent; doing it in the client is fine).
- `web/app/api/demos/[jobId]/parse/route.ts` — `POST`: body `{ steamId }` →
  proxy `POST /api/jobs/{id}/parse` `{ target_steamid: steamId }` (+token).
- `web/app/api/demos/[jobId]/plan/route.ts` — `GET`: proxy
  `GET /api/jobs/{id}/plan` → return the killplan JSON.

Handle non-2xx from the orchestrator by forwarding a JSON error + status. Never
log demo bytes. These run on the same machine as the orchestrator (local-first).

## 7. `web/app/upload/page.tsx` (rework to scan → pick → parse)

States: `idle` → (drop file) `scanning` → `picking` (roster shown) → `parsing`
→ navigate. Keep the existing `DemoDropzone`. On file:
1. `setStage('scanning')`; `const { jobId, players } = await api.scanDemo(file)`.
2. `setStage('picking')`; render a **player picker** (list/grid of `players`,
   each row: name, team chip, mono `K / D / A`, lime select ring on hover).
   Auto-highlight the top fragger but require a click to confirm.
3. On pick: `setStage('parsing')`; `const match = await api.parseDemo({ jobId,
   steamId })`; `router.push('/matches/' + match.id)`.
4. Errors at any stage: reset to idle with a readable message (scan failed /
   parse failed / target not found). Match the existing error styling.

Copy: keep "no login · the .dem never leaves your machine" (still true —
localhost). Picker heading e.g. "Who do you want to clip?".

New component `web/components/upload/player-picker.tsx` (client) — pure UI,
props `{ players: DemoPlayer[]; onPick: (steamId: string) => void }`. Mono
tabular-nums for K/D/A, lime accents per `design.md` discipline.

## 8. `web/app/(app)/matches/[id]/page.tsx` (tolerate empty score/mvps)

Real uploaded matches have `score: ''` and `mvps: 0`. Ensure the summary strip
renders cleanly when `score` is empty (hide the score chip / show
"N highlights" instead) and doesn't divide by zero. The source-aware back-nav
(upload → `/upload`) already exists; keep it.

## 9. `web/design.md` (doc)

Add a short note to the `/upload` screen section: real flow is scan → player
picker → parse; `score`/`mvps` may be empty for uploaded demos. Keep the
additive-contract note accurate (now also `scanDemo`/`parseDemo`/`DemoPlayer`).

---

# Out of scope (explicit)

- Recording/compose/render ("Create reel") for uploaded matches — needs HLAE/CS2
  + paired PC (paused). Stays mocked.
- Real Steam OpenID + CS2 match-sharing API (Flow A) — untouched, mock.
- Real `Match.score` / `mvps` (parser doesn't compute them) — fast-follow.
- Single-pass optimization (scan + segment in one read) — roadmap. MVP does two
  passes; acceptable (~1–2 min total for a 30-min demo).
- Any deploy.

# Verification (done by the orchestrator-of-this-work, not the agents)

1. Go gate green (`scripts/go-gate.sh --no-format`) + web `typecheck`/`build`.
2. Run orchestrator locally: `ZV_DATABASE_URL=memory go run ./cmd/zv-orchestrator`
   (binds `127.0.0.1:8080`; parser worker only; no HLAE).
3. Run web with `NEXT_PUBLIC_API_BASE=/api` (toggle real) on a dev port.
4. In-browser e2e (chrome-devtools): upload the repo's test `.dem`, confirm the
   real roster appears, pick a player, confirm real highlights render in
   `/matches/[id]`, console clean.
