Fix a ZackVideo bug with a regression test. Bug: $ARGUMENTS

Follow `CLAUDE.md`.

Process:

1. Run `git status --short`.
2. Reproduce or locate the bug from code/tests/logs.
3. Explain the root cause before editing.
4. Add a failing regression test that captures the bug.
5. Run the focused test and confirm the expected failure.
6. Make the smallest safe fix.
7. Run the regression test and nearby package tests.
8. Format touched Go files only.
9. If broad or risky, run `scripts/go-gate.sh --no-format` so tests, vet,
   `zv check`, and static analysis share the project contract.
10. If concurrency/shared state changed, run or recommend
    `scripts/go-gate.sh --race --no-format`.
11. Summarize root cause, fix, tests, and remaining risks.

Do not paper over failures with broad try/catch equivalents, skipped assertions,
or behavior-changing compatibility flags.
