Prepare the ZackVideo diff for review/PR. Optional focus: $ARGUMENTS

Follow `CLAUDE.md`.

Process:

1. Run `git status --short` and `git diff --stat`.
2. Inspect the diff, not just the summary.
3. Classify risk areas:
   - parser/killplan/lineups
   - media/recording/FFmpeg/Lua/overlays
   - worker/API/storage/jobs
   - concurrency/cancellation
   - security/filesystem/subprocesses
   - generated artifacts
   - dependency/schema changes
4. Ensure generated media/demo artifacts are not staged or accidentally present.
5. Run focused package tests for changed areas.
6. Run `scripts/go-format-changed.sh` unless the repo is very dirty; in that case
   format explicit touched files only.
7. Run `scripts/go-gate.sh --no-format`; it includes `zv check`.
8. If concurrency changed, also run or recommend `scripts/go-gate.sh --race`
   or `scripts/go-gate.sh --race --no-format`.
9. If security/dependencies/subprocess/filesystem behavior changed, run or
   recommend `scripts/go-gate.sh --security` or
   `scripts/go-gate.sh --security --no-format`.
10. Use relevant agents when useful:
    - `@go-readability-reviewer`
    - `@go-test-reviewer`
    - `@go-concurrency-reviewer`
    - `@go-security-reviewer`
    - `@zv-media-pipeline-reviewer`
11. Summarize files changed, behavior changed, verification commands, and risks.

Do not commit or push unless explicitly requested.
