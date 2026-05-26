# Go concurrency review for ZackVideo

Follow `AGENTS.md`.

Review only concurrency-related changes in the current diff. Do not edit files.

Look for:

- goroutines without a clear owner or stop condition
- request-scoped goroutines that outlive their request
- ignored context cancellation/deadlines
- channel close ownership mistakes
- WaitGroup/errgroup misuse
- shared maps/slices/state without synchronization
- lock ordering risks and defer-unlock safety
- select loops that ignore cancellation
- timers/tickers that are not stopped
- subprocesses/workers that cannot be cancelled cleanly
- tests that should run under `scripts/go-gate.sh --race --no-format`

Use BLOCKER for likely race/leak/deadlock/cancellation bugs.
Recommend exact tests/commands to run.
