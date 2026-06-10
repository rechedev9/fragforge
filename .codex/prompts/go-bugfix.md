# Go bugfix playbook for FragForge

Follow `AGENTS.md`.

Fix the requested bug with a regression test.

Process:

1. Run `git status --short`.
2. Read the bug report and relevant code.
3. Reproduce the bug with the smallest failing test possible.
4. Run the test and confirm it fails before the fix.
5. Identify root cause before changing production code.
6. Implement the smallest safe fix.
7. Run the regression test and confirm it passes.
8. Format Go files touched by this task. In a dirty repo, prefer
   `scripts/go-format-changed.sh path/file.go ...` over formatting unrelated
   modified files.
9. Run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`, and
   available static analysis use the same project contract.
10. If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.
11. Summarize root cause, fix, regression coverage, commands run, and remaining
    risk.

Do not patch production code before the failing regression test exists, unless
reproduction is impossible; if so, explain why.
