FragForge parser / killplan / lineups change. Task: $ARGUMENTS

Follow `CLAUDE.md`.

Use this for changes touching `.dem` parsing, kill/smoke segmentation,
`internal/parser`, `internal/killplan`, `internal/lineups`, parser CLI behavior,
or rules that produce segment plans.

Process:

1. Run `git status --short`.
2. Read relevant parser/killplan/lineup tests and fixtures before editing.
3. Identify the behavior in terms of ticks, rounds, target SteamID, event type,
   segment window, utility lineup, or output JSON contract.
4. Prefer small table-driven regression tests. Do not require large local `.dem`
   files unless the task explicitly depends on one.
5. Preserve JSON compatibility unless the user approves a contract change.
6. Be careful with off-by-one tick windows, warmup/freezetime, disconnected
   players, bot/coach entities, missing SteamIDs, team switches, and smoke/flash
   attribution.
7. Format only the Go files touched by this task.
8. Run targeted tests first, usually:
   `go test ./internal/parser ./internal/killplan ./internal/lineups ./cmd/zv-parser -count=1`
9. If broad, run `scripts/go-gate.sh --no-format`; it includes `zv check`.
10. Summarize input behavior, output contract impact, tests run, and remaining
    fixture/demo gaps.
