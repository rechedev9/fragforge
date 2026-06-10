# Review current diff for FragForge

Follow `AGENTS.md`.

Review only the current diff unless asked otherwise. Do not edit files.

Steps:

1. Run `git status --short`.
2. Inspect `git diff --stat` and relevant `git diff` hunks.
3. Review for correctness, Go readability, tests, deterministic media behavior,
   and operational risk.
4. If concurrency changed, do a dedicated race/leak/cancellation review.
5. If subprocess/filesystem/security/dependency code changed, do a security pass.

Output sections:

- BLOCKER
- WARNING
- NIT
- TEST GAPS
- COMMANDS WORTH RUNNING

For every finding include path, problem, why it matters, and suggested fix.
If the diff is good, say `No blocking issues found.`
