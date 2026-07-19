# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is a tracked symbolic link to this file, so both agents always read identical instructions.
Edit `CLAUDE.md` only; never replace the `AGENTS.md` symlink with a regular file.
The `.githooks/pre-commit` hook rejects a broken `AGENTS.md` symlink and, on `main`, runs change-aware Go/TypeScript/CI gates before a commit becomes pushable.

## Project

FragForge is a deterministic CS2 demo-to-video pipeline written primarily in Go.
It parses `.dem` files, builds kill/smoke segment plans, records gameplay with HLAE/CS2 on Windows, and post-processes clips with FFmpeg, Lua effects, overlays, and publishing metadata.
Everything runs locally on Windows.

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
- Orchestrator (`zv serve`) exposes an HTTP API and runs parser/media work on an in-process inline queue, with job state in SQLite (or in memory for tests).

Studio accepts either one demo or a multi-demo series grouped by a client-minted `series_id`.
Series flows aggregate the roster across maps so the featured player is chosen once, and HLTV-style `-pN.dem` parts are grouped as one logical map.
The inline queue keeps normal concurrency for parsing and rendering, but every HLAE/CS2 capture in one orchestrator instance shares a dedicated lane with exactly one worker because all jobs contend for the same `cs2.exe` process.

## Codex Desktop: CLI-first

When this repository is opened in Codex Desktop, the unified `zv` CLI is the
primary product interface. Codex loads these instructions through the tracked
`AGENTS.md` symlink. Do not require FragForge Studio or MCP for normal demo,
recording, render, QA, or publishing work.

Use this agent flow from the repository root in PowerShell:

1. If `bin\zv.exe` is missing or does not recognize a documented command, run
   `.\scripts\build.ps1` once to refresh the local binaries.
2. Inspect machine readiness:

   ```powershell
   .\bin\zv.exe capabilities --format json
   ```

3. Discover the end-to-end journey, then the atomic command contract:

   ```powershell
   .\bin\zv.exe flows show demo --format json
   .\bin\zv.exe flows show stream --format json
   .\bin\zv.exe workflows list --format json
   .\bin\zv.exe workflows show short --format json
   .\bin\zv.exe workflows show demo-moments --format json
   ```

4. Before execution, preflight the exact arguments:

   ```powershell
   .\bin\zv.exe workflows validate short --format json -- match.dem --prompt "all kills 76561198000000000" --dry-run --format json
   ```

5. Execute only after a successful preflight:

   ```powershell
   .\bin\zv.exe workflows run short -- match.dem --prompt "all kills 76561198000000000" --dry-run --format json
   ```

Use the staged demo journey when the user wants to choose the player or plays:
`demo players -> demo parse -> demo moments -> demo select -> record -> shorts
render`. `demo select` is the decision boundary before expensive capture and
preserves the requested narrative order. Prefer workflow `short` only when the
target and selection policy are already fully specified. The stream journey is
`stream variants -> stream plan -> stream killfeed -> stream captions -> stream
render`. Reviewed Spanish word timings make captions independent of cloud
credentials; xAI remains the automatic transcription and translation fallback.
Both journeys support `short-9x16` (1080x1920 TikTok/Shorts) and
`landscape-16x9` (1920x1080 YouTube); discover stream geometry through
`zv stream variants --format json`.
The persisted stream edit plan is the source of truth for clip order and boundaries, crop/framing, source-audio gain or mute, fades, text overlays, captions, killfeed, and music.
Use its supported API/CLI fields, including `music.volume` and `--music-volume`, instead of introducing ad hoc FFmpeg flags outside the plan.

Before any non-dry-run capture or render for a demo video, stop at the
creative brief gate. Ask the user only for choices they have not already supplied:
delivery format/aspect ratio, HUD and killfeed treatment, kill effect,
transition, kill numbering/counter, intro/outro, music, and thumbnail strategy
(`generated gameplay candidates` or `no cover`). Group the unanswered choices into one concise message, offer concrete
options supported by the CLI, and receive explicit approval. Do not treat
ambiguous execution words like "go", "hazlo", "dale", "ok", or "ya deberia
estar ok" as creative approval unless they answer a previously shown brief.
If the user says to decide autonomously before a brief exists, first state the
resolved defaults as a concrete brief and ask for approval; only a follow-up
confirmation approves the run. Preserve the approved choices in the exact
preflight argv; do not silently replace them with preset defaults later.

For stream clips, the same gate applies before any non-dry-run render. Ask for
layout/format, clip boundaries and title, clean crop/framing preference,
killfeed treatment, Spanish subtitles and review policy, music, delivery shape,
and thumbnail/cover strategy. Prefer a clean default proposal with no duplicated
killfeed overlay unless the user explicitly wants one. If captions are enabled,
generate or import Spanish caption candidates, show the reviewed text/timings
when practical, and do not call the pack final until obvious transcript
hallucinations or bad words are corrected.

Thumbnail approval is a second gate after cover candidates exist. Show the
cover sheet or candidate images, ask the user to choose one, and do not call the
pack upload-ready until a candidate is selected or the user explicitly
delegates automatic selection. A request that already names a thumbnail or
delegates its choice satisfies this gate; never repeat answered questions.

Use JSON output for decisions, preserve the exact argv reported by preflight,
and follow the existing explicit-authorization rules before HLAE/CS2 capture or
long renders. MCP is optional and disabled by default in `.codex/config.toml`;
enable it only for work that specifically needs a running Studio queue or UI.
The unified `short` and `record` commands resolve missing HLAE/CS2 paths through
the same environment/local autodetection reported by `zv capabilities`; agents
should not copy detected paths back into argv unless explicitly overriding them.

FragForge Studio also embeds a separate assistant that launches the locally installed Codex CLI through `codex app-server --stdio`.
The rail is unavailable until Codex is installed, sufficiently current, and signed in.
It runs only through the Electron preload/IPC bridge, receives a dedicated empty working directory rather than the repository or Studio data, uses a `read-only` Codex sandbox, disables generic shell/browser/web/MCP/plugin/workspace capabilities, and strips `FRAGFORGE_*`, `ZV_*`, and credential-like environment variables.
The integrated assistant can use only the allowlisted dynamic `fragforge` namespace: it searches live operations first, executes reads directly, and turns writes, costly work, and destructive operations into Studio approval previews.
Capture and render therefore have two separate gates: approval of the complete creative brief, followed by approval of the exact operation preview.
This assistant is not the optional external MCP stdio server, whose transport and file-capable operation surface are intended for explicitly configured external clients.

The orchestrator drives a job state machine: `queued -> scanning -> scanned -> parsing -> parsed -> recording -> recorded -> composing -> composed -> done` (or `failed`); jobs with a target already supplied may skip the roster-scan states.
Each worker is idempotent: it checks whether the durable artifact already exists and skips the external media command if so, which makes manual retries safe.
Pure stages (parse, compose) retry automatically; recording does not retry automatically because it costs minutes and a GPU, so it is marked `failed` for the user to decide.
A `demo_incompatible:` recording failure is deterministic and non-retryable: the current CS2 build cannot replay that demo, so keep any already captured segments and do not offer a retry that cannot change the input.
Deleting a job is a destructive domain operation allowed only after the job is settled; it removes the Studio-managed job tree and uploaded demo copy before deleting the repository row so a partial cleanup remains retryable.

Module boundaries (keep `cmd/` entrypoints thin):

- `internal/killplan` - shared kill/segment plan types; the kill plan is the contract between parse and every later stage.
- `internal/moments` - scored, reviewable clip candidates derived from kill plans.
- `internal/recording` - HLAE/CS2 recording scripts, artifacts, validation.
- `internal/editor` - Shorts rendering, the render preset registry, Lua effects, validation, publish packs.
- `internal/renderplan` - render variants, loadouts, edit documents, QA.
- `internal/composition` - concat/composition planning and FFmpeg boundaries.
- `internal/workers` - Asynq parser/media/agent workers.
- `internal/voiceprofile` - local reusable narration-reference metadata and validated audio storage; API responses expose relative audio URLs, never absolute filesystem paths.
- `internal/youtubeinsights`, `internal/youtubetrends` - deterministic Europe/Madrid scheduling, factual reel-derived metadata recommendations, and optional bounded Firecrawl trend discovery for the manual publication assistant. Firecrawl results are hints, never fabricated YouTube performance metrics.
- `web/` - standalone Next.js (App Router) frontend: upload/series, match/clip/video, stream, and news views with a typed API client; it reaches local services only through same-origin proxy routes under `/api/demos/*`, `/api/streams/*`, and `/api/news/*` (see `web/CLAUDE.md`).
- `desktop/` - Electron Local Studio packaging, process lifecycle, bundled resources, the external MCP stdio server, and the isolated integrated Codex assistant bridge.
- `data/` - generated/local media artifacts; treat as output unless the task is explicitly about test fixtures or artifact cleanup.

The current foundation runs locally and concatenates segments into `final.mp4`; treat `README.md` as the source of truth for what exists today.

## Render preset

There is a single supported preset, `viral-60-clean`, defined in `internal/editor/preset.go`.
It outputs 1080x1920 at 60fps: clean HUD-less POV with kill notices, viral hook text, kill punch-ins, a kill counter, and milestone labels.
The loadout catalog (`internal/renderplan`), the HTTP API (`/api/presets`, `/api/loadouts`, render-variant validation), the workbench UI, and the render worker all derive from that registry, and unknown preset names are rejected with the valid list.
List presets with `zv presets` (`--format json` for automation).
The editing rationale: hook text in the first 1-2s, punch-ins on kills, slow-mo only on the final kill, beat-synced drops, loop-friendly endings, never cropping the killfeed.

## Web frontend (web/)

Frontend guidance (architecture, run commands, proxy-route contract, and TypeScript style) lives in `web/CLAUDE.md`, loaded when working under `web/`.
All local API route families keep the orchestrator URL and mutation token server-side, validate IDs before constructing upstream URLs, and preserve the shared `503 {code: "service_unavailable"}` response when the local service is unavailable.
Local browser requests must use a loopback Host with an explicit port and must pass the shared Origin/Sec-Fetch-Site guard, which also prevents DNS rebinding.
Large uploads bypass body-cloning middleware but apply the same guard in the route before reading the body, stream upstream with an incremental byte limit, and keep control JSON/form bodies explicitly bounded.

## Deployment

FragForge itself ships as a local Windows desktop `.exe` (Electron, `desktop/`); there is no hosted FragForge application or backend.

The public marketing and download landing page is hosted on Vercel:

- Production URL: `https://fragforge.gravityroom.app/`
- Vercel project: `fragforge-landing`
- Vercel team: `rechedevs-projects`
- Vercel Root Directory: `landing/`
- Canonical installer assets: GitHub Releases in `rechedev9/fragforge`

Releasing a desktop version means building the installer, publishing the versioned asset to GitHub Releases, updating the landing download URL, and deploying the `landing/` project to Vercel production. Do not use the retired VPS landing deployment path or describe the landing host as undecided.

### Local Studio (web UI + local HLAE/CS2 capture)

Local Studio runs the whole product from the web UI on the user's own Windows + GPU PC, capture included, without Supabase or a paired agent.
Same-origin routes proxy demo, stream, and news operations to local services; the demo browser flow (upload -> pick player -> pick kills -> create reel) drives local HLAE/CS2 capture directly.
The news workspace currently persists source/title/hook/script drafts in browser storage and reusable voice-reference audio in FragForge's local object store.
It does not yet provide a completed news narration/render pipeline, and the voice sample is not sent to xAI, YouTube, or a voice provider.

```powershell
.\scripts\local-studio.ps1   # starts zv serve (persistent SQLite, capture auto-detected) + the bundled-style web server, opens /upload
```

This is a native Windows run, so capture works. Prerequisites: Go toolchain, Node, CS2 + HLAE installed, and the binaries built via `.\scripts\build.ps1`.
`ZV_MUSIC_DIR` defaults to `<ZV_DATA_DIR>/music`; the source Local Studio launcher provisions missing catalog tracks on a best-effort basis, discards downloads that fail SHA-256 verification, and still starts offline with whatever tracks are already available.

## Common commands

Build and run:

Build all binaries into `.\bin` with `.\scripts\build.ps1`, then:

```powershell
zv short match.dem --prompt "las mejores kills" --target-steamid 76561198000000000 --output-format short-9x16
zv short match.dem --prompt "video largo 16:9" --target-steamid 76561198000000000 --output-format landscape-16x9
zv short match.dem --prompt "all kills" --target-steamid 76561198000000000 --dry-run
zv batch C:\Users\you\Downloads\replays --recursive
zv metrics
zv errors --tail 20
zv presets
zv check
```

`zv short` chains parse -> moments -> HLAE/CS2 recording -> render and interprets the prompt deterministically (Spanish and English): a 17-digit number or `--target-steamid` selects the player, `mejores`/`best`/`highlights` selects top moments (otherwise all kills are compiled), `musica`/`music`/`beat` adds beat analysis (needs `--music`), `16:9`/`horizontal`/`video largo` selects landscape output, and an explicit preset name or `--preset` overrides the default `viral-60-clean`.
`--dry-run` resolves the plan without running anything, and rerunning a failed stage with `--from-recording <recording-result.json>` skips parse and record.
`--cover-first-frame` is opt-in for `zv short`, `zv shorts render`, and `zv-editor`; it freezes the fully composed cover frame over the opening frames so a Shorts thumbnail selector can see it, without changing duration or audio synchronization.
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
scripts/go-gate.sh --race --security --build  # full gate for risky changes
scripts/go-format-changed.sh                # format all changed Go files (or pass explicit paths)
bash scripts/ci-check.sh                    # validate GitHub Actions with pinned actionlint
```

Frontend and desktop gates:

```powershell
pnpm --dir web run typecheck
pnpm --dir web run lint
pnpm --dir web run test:unit
pnpm --dir desktop run typecheck
pnpm --dir desktop run lint
pnpm --dir desktop run test:unit
pnpm --dir desktop run build
```

The manual Electron UI E2E uses the same assembled `build-resources` layout as the installer, launches real Electron with isolated `userData`, verifies the web-to-orchestrator path, and can run beside an already open Studio instance:

```powershell
pnpm --dir desktop run build
pnpm --dir desktop run assemble
pnpm --dir desktop run test:e2e:ui
```

This Electron suite is separate from the removed browser-only `web/` Playwright suite, writes only gitignored E2E artifacts, and is not part of ordinary CI.
Current CI is Windows/desktop-first: it runs the Go gate on `windows-latest`, runs desktop lint/typecheck/unit/build, and builds the installer only when relevant paths changed.
Web, landing, optional security tools, and the Electron UI E2E rely on the change-aware local gate or explicit manual commands rather than a general CI job.

Orchestrator (HTTP API + workers) runs fully in-process with `ZV_DATABASE_URL=memory`: it uses an in-memory job repository and an inline queue, no external services needed:

```bash
ZV_DATABASE_URL=memory ZV_DATA_DIR=./data ./bin/zv serve   # in-memory job repo + inline queue
```

`zv serve` binds `127.0.0.1:8080` by default; a non-loopback bind requires `ZV_MUTATION_TOKEN`.
It also exposes `GET /healthz` (liveness) and `GET /metrics` (Prometheus text), both unauthenticated so a Prometheus server can scrape them.

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

Pipeline failures are recorded locally by `internal/obs` so they can be inspected without standing up a real Prometheus server.
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

TypeScript style for `web/` lives in `web/CLAUDE.md`.

## Operational rules

Git delivery:

- Work directly on `main`. Do not create feature branches or open pull requests.
- Direct-to-`main` is a delivery preference, not permission to commit or push; those actions still require an explicit user request.

Codex agent limit:

- When using `codex --yolo` with GPT-5.6 Sol Ultra, never spawn more than 15 sub-agents for a single user request.
  This is a total cap across the entire delegation tree, not a concurrency limit: count sub-agents spawned by the root and every descendant, including sub-agents that have already finished.

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

- The orchestrator auto-detects the capture tools on startup so the user does not set env vars: `zv-recorder` next to the orchestrator binary, the highest installed HLAE version matching `C:\HLAE-*\HLAE.exe`, and `cs2.exe` via Steam's `libraryfolders.vdf` (`internal/capturetools/detect.go`). Version comparison is numeric, not lexical. Explicit `ZV_RECORDER_PATH`/`ZV_HLAE_PATH`/`ZV_CS2_PATH` still win. `GET /api/capabilities` reports each tool's `source` (`detected`/`env`/`none`) and accessibility, which the web "Capture" card surfaces (and the record/generate handlers 409 with an actionable message when capture is not configured).
- For source/CLI capture that uses host autodetection, always use the latest official HLAE release; never pin a release number in repository instructions or command examples. Before a non-dry-run host capture, compare the version reported by `zv capabilities --format json` with the latest official AdvancedFX release and install the newer release when the local copy is stale.
- Packaged FragForge Studio instead uses the official HLAE archive pinned by `desktop/src/hlae-tool.json` for that build and accepts it only after manifest/SHA-256 verification.
  Keep packaged builds reproducible: update the manifest deliberately for a release rather than silently substituting whatever newer HLAE happens to be installed on the host.
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
