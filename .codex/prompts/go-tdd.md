# Go TDD playbook for FragForge

Follow `AGENTS.md`.

Implement the requested Go change using TDD.

Process:

1. Run `git status --short` and inspect relevant files/tests.
2. State the smallest behavior to implement.
3. Write a failing test first.
4. Run the smallest relevant test and confirm it fails for the expected reason.
5. Implement the minimum production code.
6. Run the same test and confirm it passes.
7. Add edge-case tests if needed.
8. Format Go files touched by this task. In a dirty repo, prefer
   `scripts/go-format-changed.sh path/file.go ...` over formatting unrelated
   modified files.
9. Run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`, and
   available static analysis use the same project contract.
10. If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.
11. Summarize behavior added, tests added, commands run, and remaining risk.

Do not write production code before a failing test unless the user explicitly
says this is a throwaway spike.

Do not run CS2/HLAE/long media renders unless explicitly requested.
