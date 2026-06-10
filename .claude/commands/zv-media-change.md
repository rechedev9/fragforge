FragForge media pipeline change. Task: $ARGUMENTS

Follow `CLAUDE.md`.

Use this for changes touching `internal/editor`, `internal/recording`,
`internal/composition`, Lua effects, FFmpeg command construction, HyperFrames
boundaries, captions, covers, publish packs, or media validation.

Process:

1. Run `git status --short`.
2. Read `docs/toolchain.md` and the relevant package tests before editing.
3. Identify which deterministic boundary is affected:
   - demo/killplan input
   - HLAE/CS2 capture script generation
   - FFmpeg normalization/filter graph
   - Lua effect timeline
   - overlay render/composite
   - publish pack metadata/covers/captions
4. Do not run real HLAE, CS2, Docker, DB migrations, or long FFmpeg renders
   unless explicitly requested. Prefer `--dry-run` paths and pure unit tests.
5. Add/adjust tests around command construction, manifest data, path handling,
   validation results, idempotency, and metadata. Avoid tests that need large
   media files unless the user explicitly points to a fixture.
6. Preserve deterministic output contracts: resolution, FPS, SAR, pixel format,
   audio sample rate, loudness normalization, black/freeze checks, and stable
   artifact paths.
7. Format only the Go files touched by this task.
8. Run the narrowest relevant tests first, usually one or more of:
   - `go test ./internal/editor -count=1`
   - `go test ./internal/recording -count=1`
   - `go test ./internal/composition -count=1`
   - `go test ./internal/pipeline -count=1`
9. If the change is broad, run `scripts/go-gate.sh --no-format` after targeted
   tests pass; it includes `zv check`.
10. Summarize exact media behavior changed, tests run, and remaining visual or
    toolchain risks.

Bad default: launching CS2/HLAE or rendering many videos to see what happens.
Good default: inspect generated commands/manifests and use dry-run tests.
