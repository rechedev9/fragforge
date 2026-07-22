# FragForge Agent Instructions

`AGENTS.md` is a tracked symbolic link to this file.
Edit `CLAUDE.md` only, and never replace the `AGENTS.md` symlink with a regular file.
The pre-commit hook rejects a broken link and commits made outside `main`.
This repository intentionally has no `README` files; use purpose-specific names such as `PRODUCT.md`, `GUIDE.md`, `RUNBOOK.md`, or `PROVENANCE.md`.

## Product

FragForge is a Windows-local, deterministic CS2 demo/stream-to-video pipeline written primarily in Go.
The demo is the source of truth for player, camera, tick ranges, kills, and utility; never infer recording decisions from rendered video.

```text
.dem -> parse/score -> selected kill plan -> HLAE/CS2 capture -> FFmpeg/Lua render -> publish pack
stream video -> persisted edit plan -> factual killfeed/caption review -> render -> publish pack
```

- `cmd/` contains thin entrypoints; business logic belongs under `internal/`.
- `internal/parser`, `internal/killplan`, and `internal/moments` own the durable plan passed to every later demo stage.
- `internal/recording` owns HLAE/CS2 scripts and capture validation; `internal/editor`, `internal/renderplan`, and `internal/composition` own post-capture effects, variants, QA, and FFmpeg composition.
- `effects/` contains sandboxed `gopher-lua` scripts with no filesystem or process access.
- `internal/httpapi` and `internal/workers` implement the local API and jobs; the inline queue has a dedicated one-worker capture lane because all captures contend for one `cs2.exe`.
- Workers skip completed durable artifacts on retry, but recording is enqueued with `MaxRetry(0)`; a `demo_incompatible:` failure is deterministic and must not be retried against the same CS2 build.
- Series jobs share a client-minted `series_id`; roster choice is aggregated across maps, and HLTV `-pN.dem` parts are one logical map.
- `web/` is the Next.js 15/React 19 local UI, `desktop/` packages it with the Go services in Electron, and `landing/` is the only hosted application.
- `data/`, `bin/`, capture output, and generated media are artifacts, not source, unless a task explicitly targets fixtures or cleanup.

## Codex Desktop: CLI-first

Use the unified `zv` CLI for normal parsing, capture, render, QA, and publishing; Studio is not a prerequisite.
If `bin\zv.exe` is missing or stale, run `.\scripts\build.ps1` first.

```powershell
.\bin\zv.exe capabilities --format json
.\bin\zv.exe flows show demo --format json
.\bin\zv.exe flows show stream --format json
.\bin\zv.exe workflows list --format json
.\bin\zv.exe workflows show short --format json
.\bin\zv.exe workflows validate short --format json -- match.dem --prompt "all kills 76561198000000000" --dry-run --format json
.\bin\zv.exe workflows run short -- match.dem --prompt "all kills 76561198000000000" --dry-run --format json
```

- Treat `flows show` and `workflows show` as the executable command contract; do not guess flags from prose.
- Validate the exact argv first, retain `--dry-run --format json` until real media work is approved, and preserve the approved argv when executing.
- Use `demo players -> demo parse -> demo moments -> demo select -> record -> shorts render` when the player, plays, or order still need review; use `short` only when target and selection policy are complete.
- Use `stream variants -> stream plan -> stream killfeed -> stream transcribe -> human review -> stream captions -> stream render` for VODs.
- A stream dry-run does not create its `--out` artifact; persist each approved plan before invoking a dependent stage.
- The persisted stream edit plan is canonical for ranges, order, crop, audio, fades, text, captions, killfeed, and `music.volume`; do not bolt ad hoc FFmpeg flags around it.
- Discover task-specific guidance with `.\bin\zv.exe skills list --format json` rather than duplicating skill tutorials here.
- Do not resurrect the retired external MCP server; use the CLI or the integrated typed operation gateway.
- FragForge Agent is the only assistant surface shipped in Studio.
- This project has approved FACEIT Data API access for player, match, and statistics indexing. The FACEIT Download API is not approved; obtain demo files through FACEIT's authenticated room/Watch download flow or another user-authorized manual source. Keep every FACEIT credential in environment or server-side secret storage, and never commit, print, or persist the key in indexes or logs.
- For "current" or "best performance" requests, persist the query cutoff, sample size, match IDs, filters, and ranking formula. Normalize rate statistics per round when match lengths differ. Use external statistics to shortlist demos, but use parsed demo evidence to select moments.

## Approval And Media

- Before any non-dry-run capture or render, stop at the creative brief gate and ask only unanswered choices: format, HUD/killfeed, kill effect, transition, counter, intro/outro, music, and cover strategy.
- Approval must answer a shown brief; ambiguous words such as `go`, `hazlo`, `dale`, or `ok` are not approval by themselves.
- Translate every approved brief choice into an explicit final command value, including negative booleans such as `--kill-counter=false`, `--hook=false`, and `--covers=false`; never rely on a preset or flag default to preserve an approved `off` choice. After rendering, inspect the effective result configuration and generated effects/metadata, and reject any output that re-enables a disabled element or contradicts the selected kills, weapons, rounds, or narrative.
- A successful render is not final while QA has unresolved warnings. Inspect every warning at its exact interval; remove unintended frozen, post-death, or dead-air footage, or document why it is intentional, then rerun QA.
- Any trim, reorder, or duration change invalidates existing rhythm timing. Regenerate or update the canonical rhythm plan and verify every selected kill against its assigned beat or onset before rerendering.
- For streams, also settle clip bounds/title, crop/framing, factual killfeed policy, Spanish captions and review policy, and source-audio treatment.
- Thumbnail approval is a second gate after candidates exist; require a selected candidate or explicit delegation before calling the pack upload-ready.
- Before marking a pack upload-ready, verify that the canonical MP4, title, caption, hashtags, cover, cover timestamp, gallery, manifest paths, and artifact metadata describe the same facts and files. After thumbnail selection, replace the canonical cover and visually verify the gallery again.
- `--covers=false` removes the thumbnail gate.
- Studio adds a separate approval of the exact costly/destructive operation preview; changing a stream plan invalidates its creative brief and prepared render preview.
- Local Whisper transcription produces `requires_review` evidence, not publishable words; import only verified Spanish text and clip-relative timings, or an explicit reviewed no-speech decision.
- Match imported killfeed facts to detected cues and leave unresolved events empty instead of inventing attacker, victim, weapon, or timestamps.

The preset registry in `internal/editor/preset.go` is authoritative; discover it with `.\bin\zv.exe presets --format json`.
The current default `viral-60-clean` records death notices and uses `viral-ultra-clean` effects; `clean-pov-60` removes the HUD, while `full-hud-60` preserves gameplay HUD.
HUD mode is a recording-stage choice, so changing it after capture requires recapture rather than a render-only change.
The default kill/highlight deliverable is one compiled vertical video per player/game containing all selected kills, not one upload-ready file per kill.
Put every final MP4, cover, caption, manifest, and review gallery under the run's `shortslistosparasubir/` directory, and point the user there when delivering media.
For third-party music, persist the source URL, creator, license, downloaded-file SHA-256, and rhythm-analysis evidence under the run. Never claim a track is CC0 or otherwise reusable without an authoritative source.

Do not launch HLAE, CS2, a long FFmpeg render, or paid/cloud media work without an explicit request; prefer the CLI preflight.
Host capture auto-detects the highest installed HLAE version under `C:\HLAE-*\HLAE.exe`; before a real run, compare it with the latest official HLAE release.
Never use `C:\HLAE\HLAE.exe` for FragForge capture.
Packaged Studio instead uses the SHA-256-pinned archive in `desktop/src/hlae-tool.json`; do not copy a version number into instructions or silently replace the manifest asset.
CS2 must launch through HLAE with `-windowed`; fullscreen and borderless capture are unsupported.
After final media is validated and no recapture/reparse is needed, send used extracted `.dem` files to the Windows Recycle Bin, but keep the original archive unless asked to remove it.

## Development

Toolchain sources of truth are `go.mod` (Go 1.26.5), each package's `packageManager` field (pnpm 11.9.0), and CI (Node 24).
The three JavaScript packages have independent lockfiles; run commands with `pnpm --dir web|desktop|landing`, not from an assumed root workspace.

```powershell
.\scripts\build.ps1
.\scripts\local-studio.ps1
go test ./internal/parser -run TestFoo -count=1
go test ./... -count=1 -timeout 3m
& "C:\Program Files\Git\bin\bash.exe" scripts/go-gate.sh --no-format
& "C:\Program Files\Git\bin\bash.exe" scripts/go-gate.sh --race
& "C:\Program Files\Git\bin\bash.exe" scripts/go-gate.sh --security
& "C:\Program Files\Git\bin\bash.exe" scripts/ci-check.sh
```

- Bare `bash` is a broken WSL shim on this machine; invoke shell gates through `C:\Program Files\Git\bin\bash.exe`.
- `scripts/go-gate.sh` formats changed Go files by default, then runs tests, vet, `zv check`, and optional `staticcheck`; use `--no-format` in a dirty worktree and add `--build` for the CI-equivalent Go gate.
- Add `--race` for shared-state/concurrency changes and `--security` for filesystem, subprocess, auth, or dependency-sensitive changes.
- Real-demo worker tests skip without `TEST_DEMO_PATH`; parser-only and pure unit tests must not launch HLAE/CS2 or long renders.
- Real `.dem` and generated `*.expected.json` golden files stay local; `testdata/*.rules.json` may be committed when they are reference inputs.

Run package gates in the pre-commit/CI order:

```powershell
pnpm --dir web run lint
pnpm --dir web run typecheck
pnpm --dir web run test:unit
pnpm --dir web run build
pnpm --dir desktop run lint
pnpm --dir desktop run typecheck
pnpm --dir desktop run test:unit
pnpm --dir desktop run build
pnpm --dir landing run build
```

The Electron UI E2E is manual and expensive: build, run `pnpm --dir desktop run assemble`, then `pnpm --dir desktop run test:e2e:ui` only when that product flow needs end-to-end verification.
Before frontend work, read `web/CLAUDE.md`; before visual work, also read `web/design.md`.
All browser API access must remain same-origin through server proxy routes, keep orchestrator URLs/tokens server-side, validate IDs before upstream URL construction, and preserve `503 {code: "service_unavailable"}`.
Before Electron lifecycle, embedded-agent, packaging, or release work, read `desktop/GUIDE.md` and keep renderer access behind preload/IPC plus the typed operation gateway.

## Code Contracts

- Write boring, idiomatic Go.
- Do not introduce `util`, `common`, `helper`, `manager`, or vague service layers.
- Add useful context when returning errors.
- Every goroutine must have a clear owner and stop condition.
- Add or update behavior-level tests for fixes and behavior changes.
- Do not add dependencies or run `go mod tidy` without explicit approval.
- New pipeline failure paths must record once through `internal/obs` with stable `stage` and `class` labels.
- Do not add generated video/audio/image artifacts to git.

## Git And Release

Work directly on `main`; committing or pushing still requires an explicit user request.
The change-aware `.githooks/pre-commit` gate runs project checks and package-specific lint/typecheck/test/build commands from staged paths.
FragForge has no hosted backend; the desktop release command is `pnpm --dir desktop run dist`, which verifies the bundled HLAE and emits installer checksums.
Publish versioned installer assets and `SHA256SUMS.txt` to GitHub Releases in `rechedev9/fragforge`, update the landing download URL, then deploy Vercel project `fragforge-landing` with root `landing/` to `https://fragforge.gravityroom.app/`.
Do not use the retired VPS landing path.

## Codex Harness

```bash
CODEX_DRY_RUN=1 scripts/codex-run.sh .codex/prompts/go-tdd.md "preview"
scripts/codex-go-tdd.sh "behavior change"
scripts/codex-go-bugfix.sh "bug fix"
scripts/codex-go-pr-ready.sh
```

Review findings use `BLOCKER`, `WARNING`, or `NIT` and include file/path, problem, why it matters, and a practical fix; if clean, say `No blocking issues found.`
When using `codex --yolo` with GPT-5.6 Sol Ultra, cap the entire delegation tree at 15 sub-agents.
