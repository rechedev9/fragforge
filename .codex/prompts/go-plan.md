# Go planning playbook for ZackVideo

Follow `AGENTS.md`.

Plan the requested change. Do not edit files.

Process:

1. Run `git status --short`.
2. Read the relevant code, tests, and docs.
3. Identify existing package boundaries and commands.
4. Identify risks:
   - generated media/data artifacts
   - dependencies or `go.mod`/`go.sum`
   - DB migrations/schema
   - HLAE/CS2/FFmpeg behavior
   - concurrency/shared state
   - auth/security/filesystem/subprocess execution
   - public CLI/API contracts
5. Produce a small-step implementation plan.
6. Include exact files likely to change and tests/checks to run.
7. Call out decisions that need user approval before implementation.

Output:

- Goal
- Relevant current behavior
- Proposed approach
- Step-by-step plan
- Tests/checks
- Risks/open questions

Keep the plan practical. Prefer small reversible diffs over broad rewrites.
