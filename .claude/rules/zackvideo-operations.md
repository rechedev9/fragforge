# FragForge operational rule

FragForge has expensive/external boundaries. Do not cross them casually.

Safe by default:

- `git status`, `git diff`, `git log`
- reading repo files and docs
- parser-only and pure Go unit tests
- `go test`, `go vet`, `gofmt`, `scripts/go-format-changed.sh`
- `scripts/go-gate.sh --no-format` after targeted tests pass

Ask first:

- HLAE/CS2 launch or real capture
- long FFmpeg renders
- Docker compose and database migrations
- dependency changes (`go get`, `go mod tidy`)
- git commit/push/reset/clean
- cleanup scripts that delete artifacts

Never add generated `.mp4`, `.mov`, `.webm`, `.avi`, `.mkv`, `.dem`, frame, or
large render artifacts to git.
