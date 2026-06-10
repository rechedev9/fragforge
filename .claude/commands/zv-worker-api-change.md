FragForge orchestrator / worker / API change. Task: $ARGUMENTS

Follow `CLAUDE.md`.

Use this for changes touching `internal/httpapi`, `internal/workers`,
`internal/storage`, `internal/job`, `internal/tasks`, migrations, Docker-backed
local services, or orchestrator CLI/config.

Process:

1. Run `git status --short`.
2. Read the relevant handlers/workers/storage tests before editing.
3. Identify whether the change affects:
   - public HTTP API contract
   - job state transitions
   - Asynq task payloads
   - idempotency of record/compose/retry behavior
   - durable artifact keys/paths
   - DB schema/migrations
   - Redis/Postgres dependencies
4. Do not run Docker, destructive DB commands, or migrations unless explicitly
   requested. Prefer unit tests with fakes/httptest.
5. Preserve idempotency: retries must not rerun expensive external media work
   when durable artifacts already exist.
6. Respect context cancellation around DB, Redis, HTTP, workers, and subprocesses.
7. Add regression tests for state transitions, bad inputs, idempotent retries,
   and API status codes.
8. Format only the Go files touched by this task.
9. Run targeted tests first, usually:
   `go test ./internal/httpapi ./internal/workers ./internal/storage ./internal/job ./internal/tasks -count=1`
10. If concurrency/shared state changed, run or recommend:
    `scripts/go-gate.sh --race --no-format`
11. If broad, run `scripts/go-gate.sh --no-format`; it includes `zv check`.
12. Summarize API/worker behavior changed, tests run, and migration/deployment
    risks.
