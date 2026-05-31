# Codex instructions for ZackVideo

These instructions apply to the whole repository unless a deeper AGENTS.md
overrides them.

## Project

ZackVideo is a deterministic CS2 demo-to-video pipeline written primarily in Go.
It parses `.dem` files, builds kill/smoke segment plans, records gameplay with
HLAE/CS2 on Windows, and post-processes clips with FFmpeg, Lua effects, overlays,
and publishing metadata.

Module: `github.com/reche/zackvideo`
Go version: `1.26.1`

Important boundaries:

- `cmd/`: CLI entrypoints (`zv-parser`, `zv-recorder`, `zv-composer`,
  `zv-editor`, `zv-pipeline`, `zv-orchestrator`, etc.). Keep these thin.
- `internal/parser`: `.dem` parsing and segment extraction.
- `internal/killplan`: shared kill/segment plan types.
- `internal/recording`: HLAE/CS2 recording scripts, artifacts, validation.
- `internal/composition`: concat/composition planning and FFmpeg boundaries.
- `internal/editor`: Shorts rendering, Lua effects, metadata, validation,
  publishing pack generation.
- `internal/httpapi`: orchestrator HTTP routes and handlers.
- `internal/workers`: Asynq parser/media workers.
- `internal/storage`, `internal/job`, `internal/tasks`: persistence and job state.
- `internal/lineups`: utility lineup catalog data.
- `overlays/`: HyperFrames overlay experiments and generated overlay projects.
- `data/`: generated/local media artifacts; treat as output unless the task is
  explicitly about test fixtures or artifact cleanup.

Docs worth reading before architectural changes:

- `README.md`
- `docs/toolchain.md`
- `docs/architecture/00-overview.md`
- `docs/architecture/01-components.md`
- `docs/architecture/02-data-flow.md`

## Go style

Write boring, idiomatic Go. Optimize for the reader.

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
- Protect shared maps/slices/state. Run race tests when touching shared state.

Testing:

- Bug fixes need regression tests.
- Behavior changes should add or update tests.
- Prefer table tests for business logic.
- Use `t.Run` with meaningful case names.
- Use `got` before `want` in failure messages.
- Use `t.Helper()` in helpers.
- Prefer public behavior tests over implementation-detail tests.
- Avoid tests that require real CS2/HLAE/large media unless explicitly requested.

## Operational rules for Codex

Before editing:

1. Run `git status --short`.
2. Inspect the relevant files and tests.
3. Identify whether the task touches generated media, dependencies, DB schema,
   concurrency, security/auth, or external tools.
4. Do not overwrite user changes. If a file has unrelated edits, preserve them.

During edits:

- Keep diffs small and focused.
- Do not commit, push, reset, clean, delete large directories, or rewrite history
  unless explicitly asked.
- Do not read `.env`, secrets, credentials, private keys, or local tokens.
- Do not add generated video/audio/image artifacts to git.
- Do not run HLAE, CS2, long FFmpeg renders, Docker destructive commands, or DB
  migrations unless the user explicitly asks. Prefer `--dry-run` when available.
- Parser-only Go tests and pure unit tests are safe by default.
- If a command may be slow or side-effectful, explain before running it.

Media output and cleanup:

- For CS2 Shorts, default to the most realistic demo-representative format
  available. Preserve the captured game view and full in-game UI when present
  (HUD, radar, killfeed, score, crosshair, health, ammo, and round context).
  Avoid blurred top/bottom layouts, cinematic crops, or stylized framing unless
  the user explicitly asks for that style for a specific run.
- For kill/highlight Shorts, prefer the `natural-hq2-full` preset: FFmpeg-only,
  no Lua/scripted effects, complete gameplay frame preserved in the vertical
  canvas, high-quality encode, and a subtle saturation lift to approximate the
  digital-vibrance look many CS2 players expect.
- Use `natural-hq2-full-plus` only for explicit A/B tests: it keeps the same
  full-UI layout but adds stronger color, light sharpening, CRF 15, slower
  x264 encoding, and BT.709 metadata.
- Put every final, upload-ready recording, Shorts pack, long compilation, cover,
  caption, manifest, and review sheet under a folder named
  `shortslistosparasubir` inside the run output directory. Intermediate capture,
  parser, recorder, render, and log artifacts may remain in their normal
  run-specific folders.
- Final responses should point the user to the `shortslistosparasubir` folder or
  to files inside it when delivering finished media.
- After `.dem` files have been parsed/recorded and the final upload-ready media
  has been validated, clean up the used `.dem` files by sending them to the
  Windows Recycle Bin, not by permanently deleting them.
- If demos were extracted from an archive, recycle only the extracted `.dem`
  copies by default. Keep the original downloaded archive unless the user
  explicitly asks to remove it.
- Do not recycle `.dem` files until no further rerender, recapture, or parsing
  step needs them. If that is unclear, keep them and mention the pending cleanup.

Local capture path:

- Use `C:\HLAE-2.190.1\HLAE.exe` for HLAE capture on this machine.
- Do not use `C:\HLAE\HLAE.exe`; it is the wrong HLAE install for ZackVideo
  capture runs.

Verification:

- Format all changed Go files: `scripts/go-format-changed.sh`
- Format only specific files in a dirty repo: `scripts/go-format-changed.sh path/file.go ...`
- Main gate: `scripts/go-gate.sh`
- Gate without formatting unrelated dirty files: `scripts/go-gate.sh --no-format`
- Race-sensitive changes: `scripts/go-gate.sh --race`
- Security/dependency-sensitive changes: `scripts/go-gate.sh --security`
- Full command for risky PRs: `scripts/go-gate.sh --race --security --build`

If an optional tool is missing (`goimports`, `staticcheck`, `govulncheck`,
`gosec`), say so and continue with the available checks.

## Codex task harness

Reusable prompt playbooks live under `.codex/prompts/`.

Run them through:

- `scripts/codex-run.sh .codex/prompts/go-tdd.md "custom prompt run"` for a
  direct prompt-file run.
- `scripts/codex-plan.sh "plan ..."` for read-only planning.
- `scripts/codex-go-tdd.sh "implement ..."` for behavior changes.
- `scripts/codex-go-bugfix.sh "fix ..."` for bugs.
- `scripts/codex-spike.sh "test whether ..."` for reversible experiments.
- `scripts/codex-go-pr-ready.sh` before review/PR.
- `scripts/codex-review-diff.sh` for read-only review.
- `scripts/codex-go-readability-review.sh` for focused readability review.
- `scripts/codex-go-test-review.sh` for focused test review.
- `scripts/codex-go-concurrency-review.sh` for goroutine/race/cancellation review.
- `scripts/codex-go-security-review.sh` for filesystem/subprocess/security review.

The wrappers feed Codex a selected prompt plus task text. Write-oriented scripts
default to `workspace-write`; planning/review scripts default to `read-only`.
Use `CODEX_DRY_RUN=1` to preview the final prompt without calling Codex.

## Review output standard

For reviews, use:

- `BLOCKER`: correctness, data loss, security, broken tests, production safety.
- `WARNING`: real maintainability or robustness issue, but possibly acceptable.
- `NIT`: small clarity/style improvement.

Every finding should include file/path, problem, why it matters, and a practical
fix. If the diff is good, say: `No blocking issues found.`
