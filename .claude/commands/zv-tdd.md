Implement a ZackVideo behavior change with TDD. Task: $ARGUMENTS

Follow `CLAUDE.md`.

Process:

1. Run `git status --short` and avoid overwriting unrelated user changes.
2. Inspect the relevant package and tests before editing.
3. Define the expected behavior and the smallest regression/behavior test.
4. Write or update the failing test first.
5. Run the narrow focused test and confirm it fails for the expected reason.
6. Implement the smallest production change.
7. Run the focused test until it passes.
8. Format only touched Go files with `scripts/go-format-changed.sh <files...>`.
9. Run targeted package tests.
10. If broad, run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`,
    and static analysis share the project contract.
11. If concurrency/shared state changed, run or recommend
    `scripts/go-gate.sh --race --no-format`.
12. Summarize behavior changed, tests run, and remaining risks.

Do not run HLAE, CS2, Docker, DB migrations, long FFmpeg renders, or dependency
changes unless explicitly requested.
