# FragForge operational rule

FragForge has expensive/external boundaries. Do not cross them casually.

Safe by default:

- `git status`, `git diff`, `git log`
- reading repo files and docs
- parser-only and pure Go unit tests
- `go test`, `go vet`, `gofmt`, `scripts/go-format-changed.sh`
- `scripts/go-gate.sh --no-format` after targeted tests pass
- `zv batch <dir>` (in-process demo parsing only), `zv metrics`, `zv errors`

Observability:

- Pipeline failures are recorded by `internal/obs` to `$ZV_DATA_DIR/obs`
  (`journal.jsonl` plus Prometheus `metrics.prom`); the orchestrator serves the
  same counters at `/metrics` and a `/healthz` probe.
- Prefer `zv batch` to drive a folder of demos and read the error log over
  invoking the per-demo CLI by hand; the loop is run -> read `zv errors` -> fix
  -> rerun until the log is empty.
- When adding a stage or failure path, record it through `internal/obs` with a
  stable `stage` and `class` label, not just a log line.
- `$ZV_DATA_DIR/obs` is generated output; never commit it (`**/data/obs/` is
  gitignored).

Ask first:

- HLAE/CS2 launch or real capture
- long FFmpeg renders
- Docker compose and database migrations
- dependency changes (`go get`, `go mod tidy`)
- git commit/push/reset/clean
- cleanup scripts that delete artifacts

Never add generated `.mp4`, `.mov`, `.webm`, `.avi`, `.mkv`, `.dem`, frame, or
large render artifacts to git.
