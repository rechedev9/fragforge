# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is auto-generated from this file by the `.githooks/pre-commit` hook, so both agents read identical instructions.
Edit `CLAUDE.md` only.
Do not edit `AGENTS.md` by hand; any direct change is overwritten on the next commit.

## Project

FragForge is a deterministic CS2 demo-to-video pipeline written primarily in Go.
It parses `.dem` files, builds kill/smoke segment plans, records gameplay with HLAE/CS2 on Windows, and post-processes clips with FFmpeg, Lua effects, overlays, and publishing metadata.
Everything runs locally on Windows.

Module: `github.com/rechedev9/fragforge`
Go version: `1.26.1`

## Architecture

The pipeline is a chain of independent stages connected by on-disk artifacts and a job queue, so each stage is testable and replaceable on its own.
The demo is the source of truth: every decision about what to record (tick ranges, camera, player) is derived from parsing the `.dem`, never from heuristics over the rendered video.

End-to-end flow:

```text
.dem + prompt
  -> parse demo into a kill plan and scored moments   (internal/parser, internal/killplan, internal/moments)
  -> record the chosen segments with HLAE/CS2          (internal/recording, HLAE mirv JS script)
  -> render the Short with a viral preset              (internal/editor, internal/renderplan, Lua effects, FFmpeg)
  -> publish pack: MP4, cover, caption, gallery, manifest
```

Recording and composition are deliberately split: HLAE/CS2 only produces high-quality raw segments, and all effects (zoom, flash, slow-mo, color grade, music) are applied afterward in an FFmpeg composition stage.
The clip "look" lives in editable Lua scripts under `effects/`, evaluated by a sandboxed `gopher-lua` DSL (`on_segment`, `on_kill`, `on_smoke`, `zoom`, `flash`, `text`, `grade`) with no filesystem/process access and a capped budget.

Two ways to run the pipeline:

- CLI (`zv short`, or the granular stage commands) runs the whole chain in-process on the local machine.
- Orchestrator (`zv serve`) exposes an HTTP API and runs parser/media work on Asynq workers backed by Redis, with job state in Postgres.

The orchestrator drives a job state machine: `queued -> parsing -> parsed -> recording -> recorded -> composing -> composed -> done` (or `failed`).
Each worker is idempotent: it checks whether the durable artifact already exists and skips the external media command if so, which makes manual retries safe.
Pure stages (parse, compose) retry automatically; recording does not retry automatically because it costs minutes and a GPU, so it is marked `failed` for the user to decide.

Module boundaries (keep `cmd/` entrypoints thin):

- `cmd/` - CLI entrypoints (`zv` unified binary plus `zv-parser`, `zv-recorder`, `zv-composer`, `zv-editor`, `zv-pipeline`, `zv-orchestrator`, ...).
- `internal/parser` - `.dem` parsing and segment extraction (built on `markus-wa/demoinfocs-golang`).
- `internal/killplan` - shared kill/segment plan types; the kill plan is the contract between parse and every later stage.
- `internal/moments` - scored, reviewable clip candidates derived from kill plans.
- `internal/recording` - HLAE/CS2 recording scripts, artifacts, validation.
- `internal/editor` - Shorts rendering, the render preset registry, Lua effects, validation, publish packs.
- `internal/renderplan` - render variants, loadouts, edit documents, QA.
- `internal/composition` - concat/composition planning and FFmpeg boundaries.
- `internal/httpapi` - orchestrator HTTP routes, handlers, and the embedded HTMX workbench UI.
- `internal/workers` - Asynq parser/media/agent workers.
- `internal/storage`, `internal/job`, `internal/tasks` - persistence and job state.
- `internal/lineups` - utility lineup catalog data.
- `effects/` - editable Lua effect scripts.
- `overlays/` - HyperFrames overlay experiments and generated overlay projects.
- `web/` - standalone Next.js (App Router) frontend: the no-login `/upload` flow, match/clip/video views, and a typed API client; it reaches the orchestrator only through same-origin `/api/demos/*` proxy routes (see "Web frontend" below).
- `services/cs2-market` - separate Python prototype for CS2 item market research, with its own CLI (`cs2market init-db`, `ingest`, `score`, `export-shorts`).
- `data/` - generated/local media artifacts; treat as output unless the task is explicitly about test fixtures or artifact cleanup.

Note: `docs/architecture/*` describes the full design vision (object storage, a separate music mixer and encoder, a fuller web frontend than `web/` ships today).
The current foundation runs locally and concatenates segments into `final.mp4`; treat the README as the source of truth for what exists today.

Docs worth reading before architectural changes:

- `README.md`
- `docs/toolchain.md`
- `docs/architecture/00-overview.md`
- `docs/architecture/01-components.md`
- `docs/architecture/02-data-flow.md`
- `docs/specs/` for the specs that produced this code.

## Render preset

There is a single supported preset, `viral-60-clean`, defined in `internal/editor/preset.go`.
It outputs 1080x1920 at 60fps: clean HUD-less POV with kill notices, viral hook text, kill punch-ins, a kill counter, and milestone labels.
The loadout catalog (`internal/renderplan`), the HTTP API (`/api/presets`, `/api/loadouts`, render-variant validation), the workbench UI, and the render worker all derive from that registry, and unknown preset names are rejected with the valid list.
List presets with `zv presets` (`--format json` for automation).
The editing rationale (hook text in the first 1-2s, punch-ins on kills, slow-mo only on the final kill, beat-synced drops, loop-friendly endings, never cropping the killfeed) is documented in `docs/research/11-viral-cs2-vertical-editing.md`.

## Web frontend (web/)

`web/` is a standalone Next.js app (App Router, React 19, Tailwind 4): the no-login `/upload` entry, match/clip/video views, and a typed API client under `web/lib/api`.
It is local-first and stateless: it talks only to the orchestrator (`zv serve`) through same-origin proxy route handlers under `web/app/api/demos/*`, which forward `.dem` uploads and job calls while keeping the orchestrator URL and token server-side.
`web/lib/api` uses the real client when `NEXT_PUBLIC_API_BASE` is set (see `web/.env.local`), otherwise an in-memory mock.

Run it locally (needs the orchestrator on `127.0.0.1:8080`; orchestrator memory mode is enough for the upload -> roster -> parse path):

```bash
cd web && npm install && npm run dev   # http://localhost:3000
npm run typecheck                      # tsc --noEmit
npm run test:e2e                       # Playwright e2e
```

Proxy-route contract: every `/api/demos/*` route reaches the orchestrator through `callOrchestrator` (`web/app/api/demos/_lib.ts`).
When the orchestrator is unreachable the route returns `503 {code: "service_unavailable"}` and logs the cause server-side, and the UI tells "service offline" apart from a bad demo via `SERVICE_UNAVAILABLE_CODE`.
Keep that contract when adding `/api/demos/*` routes; do not let a bare `fetch` throw into a code-less 500.

E2E lives in `web/e2e` (`playwright.config.ts`, `@playwright/test`); run it with `npm run test:e2e`.
The error-messaging specs mock the network and need only the dev server.
The happy-path spec uploads a real demo and is gated on a reachable orchestrator plus a demo at `ZV_E2E_DEMO` (default `../testdata/sample.dem`), so it skips rather than fails when either is absent.
Real `.dem` files are never committed, so the fixture stays local.

## Deployment

There is no hosted/CI deploy in the repo: no GitHub Actions, no Vercel/Netlify/Fly config, no deploy scripts.
"Production" therefore means: merge the working branch into `main` and push.
A Vercel/Railway project may be connected to the GitHub repo and auto-deploy on push to `main`, but that cannot be verified from the clone, so confirm in the dashboard.
Do not invent a VPS or Vercel setup; if real hosting is wanted, treat it as an explicit infra task and ask for the target.

### Local Studio (web UI + local HLAE/CS2 capture)

Local Studio runs the whole product from the web UI on the user's own Windows + GPU PC, capture included, without Supabase or a paired agent.
The web proxies the entire `/api/demos/*` pipeline to a local orchestrator (`zv serve`) on the same machine, so the browser flow (upload -> pick player -> pick kills -> create reel) drives local HLAE/CS2 capture directly.

```powershell
.\scripts\local-studio.ps1   # starts zv serve (memory mode, capture auto-detected) + the web in local mode, opens /upload
```

One flag selects the data plane, `NEXT_PUBLIC_FRAGFORGE_MODE` (default `cloud`):

- `local`: the web talks only to the local orchestrator; scan/status/roster proxy to it (`web/app/api/demos/_local.ts`), and the rest of the pipeline (parse/plan/record/renders/capabilities) already does.
  A single orchestrator job UUID flows through scan -> parse -> record -> render, so the record button captures with the job that scan created.
- `cloud`: uploads and scan go to Supabase and a paired desktop agent captures (the hosted control-plane).

Unlike the Docker stack below, this is a native Windows run, so capture works.
See [`docs/local-studio.md`](docs/local-studio.md) for prerequisites and what the flag switches.

### Local Docker stack

A two-container stack runs the web UI and the orchestrator together for a local deployment:

```bash
docker compose -f docker-compose.app.yml up --build
# web UI:        http://localhost:3000   (the no-login /upload analyze flow)
# orchestrator:  http://127.0.0.1:8080   (loopback-only, e.g. curl /api/capabilities)
```

Files: `Dockerfile` (orchestrator, multi-stage Go build into a distroless static image), `web/Dockerfile` (Next.js standalone; `NEXT_PUBLIC_API_BASE=/api` baked at build), and `docker-compose.app.yml` (wires the two).
This is separate from the dev `docker-compose.yml`, which only provides Postgres+Redis for `make up`.

Scope and constraints:

- It does NOT do gameplay capture. HLAE + CS2 are Windows + GPU and cannot run in a Linux container, so the orchestrator runs in memory mode (in-memory job repo + inline queue, no Postgres/Redis) and serves the analyze flow only (upload -> scan roster -> scoreboard -> pick player -> match/highlights).
- Because capture is unconfigured, the sidebar "Capture" card correctly reads "Set up capture" and a created reel surfaces a clear "recording is not configured" failure. Real capture (and rendering captured footage) still needs a host orchestrator with `ZV_RECORDER_PATH`/`ZV_HLAE_PATH`/`ZV_CS2_PATH` set (see Common commands and the HLAE path under Operational rules).
- The orchestrator binds `0.0.0.0:8080` inside its container; a non-loopback bind requires `ZV_MUTATION_TOKEN` (defaults to `fragforge-local`, override via the `ZV_MUTATION_TOKEN` env). The web container reaches it at `ORCHESTRATOR_URL=http://orchestrator:8080` and sends the same token as `ORCHESTRATOR_TOKEN`; the `/api/demos/*` proxy carries the token on reads and writes.
- Demo blobs and artifacts persist in the `appdata` volume; jobs live in memory and reset on orchestrator restart. For the persistent Postgres/Redis mode instead, use the dev `docker-compose.yml` plus the orchestrator env in Common commands.

## Common commands

Build and run:

Build all binaries into `.\bin` with `.\scripts\build.ps1`, then:

```powershell
zv short match.dem --prompt "las mejores kills" --target-steamid 76561198000000000
zv short match.dem --prompt "all kills" --target-steamid 76561198000000000 --dry-run
zv batch C:\Users\you\Downloads\replays --recursive
zv metrics
zv errors --tail 20
zv presets
zv check
```

`zv short` chains parse -> moments -> HLAE/CS2 recording -> render and interprets the prompt deterministically (Spanish and English): a 17-digit number or `--target-steamid` selects the player, `mejores`/`best`/`highlights` selects top moments (otherwise all kills are compiled), `musica`/`music`/`beat` adds beat analysis (needs `--music`), and an explicit preset name or `--preset` overrides the default `viral-60-clean`.
`--dry-run` resolves the plan without running anything, and rerunning a failed stage with `--from-recording <recording-result.json>` skips parse and record.
`zv check` sanity-checks the project contract (skills, workflows, docs); `zv presets` lists render presets.
`zv batch`, `zv metrics`, and `zv errors` are the error-tracking commands (see Observability below).

Tests and the verification gate:

```bash
make test                                   # go test ./... -count=1 plus `zv check`
go test ./... -count=1                      # all Go tests
go test ./internal/parser -run TestFoo -count=1   # a single test (parser-only tests are safe by default)
go test ./... -race                         # race detector for shared-state changes
scripts/go-gate.sh                          # main gate: fmt, vet, build, tests
scripts/go-gate.sh --no-format              # gate without formatting unrelated dirty files
scripts/go-gate.sh --race                   # race-sensitive changes
scripts/go-gate.sh --security               # security/dependency-sensitive changes
scripts/go-gate.sh --race --security --build  # full gate for risky PRs
scripts/go-format-changed.sh                # format all changed Go files (or pass explicit paths)
```

Orchestrator (HTTP API + workers), needs Docker for local Postgres and Redis:

```bash
make up                                      # Postgres + Redis via Docker
make migrate-up                              # needs ZV_DATABASE_URL exported
export ZV_DATABASE_URL="postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable"
export ZV_REDIS_ADDR="localhost:6379"
export ZV_DATA_DIR="./data"
zv serve
```

`zv serve` binds `127.0.0.1:8080` by default; a non-loopback bind requires `ZV_MUTATION_TOKEN`.
It also exposes `GET /healthz` (liveness) and `GET /metrics` (Prometheus text), both unauthenticated so a Prometheus server can scrape them.

For local development without Docker, run the orchestrator fully in-process with `ZV_DATABASE_URL=memory`: it uses an in-memory job repository and auto-switches the queue to inline mode, so no Postgres and no Redis are needed.

```bash
ZV_DATABASE_URL=memory ZV_DATA_DIR=./data ./bin/zv serve   # in-memory job repo + inline queue, no Postgres/Redis
```

This is enough for the parse and roster-scan stages (the parser worker is always registered); the record, compose, and render workers only start when their tool paths are set (`ZV_RECORDER_PATH`, `ZV_HLAE_PATH`, `ZV_CS2_PATH`, `ZV_EDITOR_PATH`, `ZV_FFMPEG_PATH`).
This is the orchestrator the web frontend talks to during local upload/parse work.

Smoke tests:

```bash
./scripts/smoke.sh testdata/<demo>.dem <SteamID64>          # parser-only
```

```powershell
.\scripts\smoke-real.ps1 -Demo testdata\<demo>.dem -TargetSteamID <SteamID64>   # full real run against a running orchestrator
```

If an optional tool is missing (`goimports`, `staticcheck`, `govulncheck`, `gosec`), say so and continue with the available checks.

## Observability and error tracking

Pipeline failures are recorded locally by `internal/obs` so they can be inspected without standing up Postgres, Redis, or a real Prometheus server.
The recorder writes two artifacts under `$ZV_DATA_DIR/obs` (default `data/obs`):

- `journal.jsonl` - one JSON line per error: time, stage, class, message, demo, target, exit code.
- `metrics.prom` - Prometheus text exposition of `fragforge_stage_runs_total{stage,result}` and `fragforge_errors_total{stage,class}` (with `metrics.json` as the reload sidecar).

Failures are recorded at the orchestration boundaries, each owning its recording so counts are not doubled: `zv batch` (in-process parse), `zv short` stage failures, and the Asynq worker terminal failures (`recordTaskFailure`).
The orchestrator also serves the same counters at `GET /metrics` for a Prometheus scrape, and `GET /healthz` for liveness.

The autonomous loop (the goal of `zv batch`) is: run a folder of demos, read the error log, fix what it surfaces, rerun, until the log is empty.

```bash
zv batch testdata --recursive --report data/obs/batch-report.json
zv errors --tail 50
zv metrics
zv errors --clear
```

`zv batch <dir>` parses every `.dem` under `<dir>` in-process (auto-picking the top fragger as the target unless `--steamid` is given), records each failure, and exits non-zero when any demo failed, so a fix loop can detect a non-empty log.
When adding a new pipeline stage or failure path, record its errors through `internal/obs` with a stable `stage` and `class` label rather than only logging, so it shows up in the same journal and metrics.

## Go style

Write boring, idiomatic Go.
Optimize for the reader.
Repo-local style rules are also mirrored in `.claude/rules/go-style.md`.

Priorities, in order:

1. Clarity
2. Simplicity
3. Concision
4. Maintainability
5. Consistency with this repo

Rules:

- Prefer concrete code over premature abstractions.
- Do not introduce `util`, `common`, `helper`, `manager`, or vague service layers.
- Do not translate Java/Spring/.NET patterns into Go.
- Keep interfaces small and define them at the consumer side when useful.
- Do not add an interface just to mock one dependency.
- Keep exported APIs small and documented.
- Avoid global mutable state.
- Do not add dependencies or run `go mod tidy` without explicit approval.

Errors:

- Return errors; do not panic for normal control flow.
- Error strings should be lowercase and have no trailing punctuation.
- Add useful context when returning errors.
- Use `fmt.Errorf("...: %w", err)` when callers may unwrap.
- Do not log and return the same error unless at a process/API boundary.
- Do not discard errors with `_` unless a comment explains why it is safe.

Context and concurrency:

- `context.Context` is normally the first parameter.
- Do not store `context.Context` in structs.
- Every goroutine must have a clear owner and stop condition.
- Respect cancellation/deadlines around DB, Redis, HTTP, subprocesses, and workers.
- Protect shared maps/slices/state, and run race tests when touching shared state.

Testing:

- Bug fixes need regression tests.
- Behavior changes should add or update tests.
- Prefer table tests for business logic.
- Use `t.Run` with meaningful case names.
- Use `got` before `want` in failure messages.
- Use `t.Helper()` in helpers.
- Prefer public behavior tests over implementation-detail tests.
- Avoid tests that require real CS2/HLAE/large media unless explicitly requested.

## TypeScript style (web/)

The web frontend is full TypeScript under `strict: true`; the complete rules live in `.claude/rules/typescript-style.md` (adapted from the jvidalv/berrus guidelines).
The load-bearing ones:

- No `any` in any form (`as any`, `<any>`, `any[]`); use `unknown` plus narrowing when a shape is genuinely unknown.
- No `!` non-null assertions and no `as <Type>` to silence the checker; casts are allowed only at trust boundaries (`JSON.parse`, `res.json()`, storage, env) to a named type, with untrusted input validated before use.
- No re-exports and no backwards-compat shims; when moving code, update every import.
- No magic strings across boundaries; use named `const`s or `as const` maps with derived union types (`SERVICE_UNAVAILABLE_CODE` is the house example).
- `Promise.all` for independent awaits; sequential `await` of independent operations is a performance bug.
- Secrets stay server-side (route handlers, `server-only`); never in client components or `NEXT_PUBLIC_*`.
- `npm run typecheck` must pass before a web change is done.

## Operational rules

Before editing:

1. Run `git status --short`.
2. Inspect the relevant files and tests.
3. Identify whether the task touches generated media, dependencies, DB schema, concurrency, security/auth, or external tools.
4. Do not overwrite user changes; if a file has unrelated edits, preserve them.

During edits:

- Keep diffs small and focused.
- Do not commit, push, reset, clean, delete large directories, or rewrite history unless explicitly asked.
- Do not read `.env`, secrets, credentials, private keys, or local tokens.
- Do not add generated video/audio/image artifacts to git.
- Do not run HLAE, CS2, long FFmpeg renders, Docker destructive commands, or DB migrations unless the user explicitly asks; prefer `--dry-run` when available.
- Parser-only Go tests and pure unit tests are safe by default.
- If a command may be slow or side-effectful, explain before running it.

Media output and delivery:

- For CS2 Shorts, default to the most realistic demo-representative format available, preserving the captured game view and full in-game UI when present (HUD, radar, killfeed, score, crosshair, health, ammo, round context).
  Avoid blurred top/bottom layouts, cinematic crops, or stylized framing unless the user explicitly asks for that style for a specific run.
- For kill/highlight Shorts, use the product default preset `viral-60-clean` and the `viral-ultra-clean` overlay pack; do not use or advertise alternate presets.
- For kill/highlight deliverables, default to one long vertical Short per player/game containing all selected kills.
  Per-kill Shorts may be rendered as intermediate inputs, but the upload-ready default is the concatenated all-kills long Short unless the user explicitly asks for individual per-kill Shorts.
- Put every final, upload-ready recording, Shorts pack, long compilation, cover, caption, manifest, and review sheet under a folder named `shortslistosparasubir` inside the run output directory.
  Intermediate capture, parser, recorder, render, and log artifacts may stay in their normal run-specific folders.
- Final responses should point the user to the `shortslistosparasubir` folder or to files inside it when delivering finished media.

Demo cleanup:

- After `.dem` files have been parsed/recorded and the final upload-ready media has been validated, clean up the used `.dem` files by sending them to the Windows Recycle Bin, not by permanently deleting them.
- If demos were extracted from an archive, recycle only the extracted `.dem` copies by default; keep the original downloaded archive unless the user explicitly asks to remove it.
- Do not recycle `.dem` files until no further rerender, recapture, or parsing step needs them; if that is unclear, keep them and mention the pending cleanup.

Local capture path:

- The orchestrator auto-detects the capture tools on startup so the user does not set env vars: `zv-recorder` next to the orchestrator binary, HLAE at `C:\HLAE-*\HLAE.exe`, and `cs2.exe` via Steam's `libraryfolders.vdf` (`cmd/zv-orchestrator/detect.go`). Explicit `ZV_RECORDER_PATH`/`ZV_HLAE_PATH`/`ZV_CS2_PATH` still win. `GET /api/capabilities` reports each tool's `source` (`detected`/`env`/`none`) and accessibility, which the web "Capture" card surfaces (and the record/generate handlers 409 with an actionable message when capture is not configured).
- Use `C:\HLAE-2.190.1\HLAE.exe` for HLAE capture on this machine; auto-detection prefers a versioned `C:\HLAE-*` over the bare `C:\HLAE\HLAE.exe`.
- Do not use `C:\HLAE\HLAE.exe`; it is the wrong HLAE install for FragForge capture runs.
- Always launch CS2 through HLAE in windowed mode for recording runs; the CS2 command line must include `-windowed`, and demos must not be recorded in fullscreen or borderless fullscreen.

## Task harnesses

Claude Code: repo-local slash commands live under `.claude/commands/` and reviewer agents under `.claude/agents/`.

- `/zv-plan <task>` for read-only planning.
- `/zv-tdd <task>` for behavior changes.
- `/zv-bugfix <task>` for bugs.
- `/zv-parser-change <task>` for parser/killplan/lineup changes.
- `/zv-media-change <task>` for editor/recording/FFmpeg/Lua changes.
- `/zv-worker-api-change <task>` for orchestrator/API/worker changes.
- `/zv-pr-ready` before review/PR.
- `/zv-artifact-audit` for read-only generated artifact audits.
- `/zv-toolchain-diagnose` for read-only local toolchain checks.
- `@go-readability-reviewer`, `@go-test-reviewer`, `@go-concurrency-reviewer`, `@go-security-reviewer`, `@zv-media-pipeline-reviewer` for focused diff reviews.

Non-interactive wrappers call Claude Code print mode with the same playbooks: `scripts/claude-run.sh .claude/commands/zv-tdd.md "..."` runs a command file directly, and the focused wrappers are `scripts/claude-zv-tdd.sh "..."` (behavior changes), `scripts/claude-zv-bugfix.sh "..."` (bugs), and `scripts/claude-zv-pr-ready.sh` (before review/PR).
`CLAUDE_DRY_RUN=1` previews the final prompt without calling Claude Code.

Codex: reusable prompt playbooks live under `.codex/prompts/`, run through `scripts/codex-*.sh`.

- `scripts/codex-run.sh .codex/prompts/go-tdd.md "..."` runs a prompt file directly.
- `scripts/codex-plan.sh "..."` for read-only planning.
- `scripts/codex-go-tdd.sh "..."` for behavior changes.
- `scripts/codex-go-bugfix.sh "..."` for bugs.
- `scripts/codex-spike.sh "..."` for reversible experiments.
- `scripts/codex-go-pr-ready.sh` before review/PR.
- `scripts/codex-review-diff.sh` and the focused `scripts/codex-go-*-review.sh` scripts for read-only reviews.

Write-oriented Codex scripts default to `workspace-write`; planning/review scripts default to `read-only`; `CODEX_DRY_RUN=1` previews the final prompt.

## Review output standard

For reviews, use:

- `BLOCKER`: correctness, data loss, security, broken tests, production safety.
- `WARNING`: real maintainability or robustness issue, but possibly acceptable.
- `NIT`: small clarity/style improvement.

Every finding should include file/path, problem, why it matters, and a practical fix.
If the diff is good, say: `No blocking issues found.`
