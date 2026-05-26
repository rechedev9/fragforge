# Security and Performance Audit

Score: **5/5**

Date: 2026-05-23

## Gates

The score is based on the repo passing these gates:

- `scripts/fix-loop.ps1`
  - `gofmt`
  - `go vet ./...`
  - `go test ./... -count=1`
  - `scripts/build.ps1`
  - generated media/demo check
- `gosec ./...`
- `govulncheck ./...`
- `go test ./... -run=^$ -bench=. -benchmem`
- `git diff --check`

On Windows, `go test -race ./...` requires CGO and a C compiler such as GCC.
This workstation uses Scoop's GCC package:

```powershell
scoop install gcc
$env:CGO_ENABLED = "1"
go test -race ./...
```

The race gate is available through
`scripts/audit-security-performance.ps1 -Race`. The script skips it only if a C
compiler is missing from `PATH`.

## Fixes Applied

- HTTP upload parsing now caps the full multipart request body before parsing.
- Local storage key resolution now normalizes paths, rejects absolute paths, and
  rejects traversal without blocking valid file names that contain dots.
- Production directory/file permissions were tightened from `0755`/`0644` to
  `0750`/`0600`.
- `zv-parser` now checks stdout write errors.
- Static security false positives for local CLI subprocesses and explicit local
  file inputs are annotated with `#nosec` and a reason.
- Windows drawtext font discovery no longer uses a tainted environment variable
  path.

## Current Position

Security is rated 5/5 because reachable vulnerability scanning is clean, static
security scanning is clean, upload/body limits are explicit, storage traversal is
tested, and subprocess/file-input warnings are either fixed or documented as
local-tooling behavior.

Performance is rated 5/5 for the current scope because the network upload path
streams directly to storage while hashing, normal tests and benchmark compilation
pass across all packages, and no generated media/demo artifacts remain in the
repo tree. The project still lacks dedicated microbenchmarks for parser/editor
hot paths; add those once realistic fixture data is checked into an external
benchmark dataset.
