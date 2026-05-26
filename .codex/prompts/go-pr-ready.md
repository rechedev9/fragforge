# Go PR-ready playbook for ZackVideo

Follow `AGENTS.md`.

Prepare the current change for review. Do not commit unless explicitly asked.

Process:

1. Run `git status --short` and inspect `git diff`.
2. Identify whether the diff touches:
   - generated media/data artifacts
   - dependencies or `go.mod`/`go.sum`
   - DB migrations/schema
   - auth/security/filesystem/subprocess execution
   - goroutines/channels/shared state
   - public CLI/API contracts
   - HLAE/CS2/FFmpeg behavior
3. Format changed Go files. If the repo contains unrelated dirty work, format
   only the files in scope with `scripts/go-format-changed.sh path/file.go ...`.
4. Run `scripts/go-gate.sh`; use `scripts/go-gate.sh --no-format` if you
   already formatted the in-scope files and must avoid touching unrelated work.
5. If concurrency/shared state changed, run `scripts/go-gate.sh --race`.
6. If security/dependencies/subprocess/filesystem code changed, run
   `scripts/go-gate.sh --security`.
7. Review the final diff using the standards in `AGENTS.md`.
8. Summarize:
   - files changed
   - behavior changed
   - tests added/changed
   - commands run and results
   - blocker/warning/nit findings
   - remaining risk

Do not hide failing checks. If a check fails because of existing unrelated work,
separate that clearly from issues introduced by this task.
