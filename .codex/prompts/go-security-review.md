# Go security review for ZackVideo

Follow `AGENTS.md`.

Review only security-relevant changes in the current diff. Do not edit files.

Focus on:

- path traversal or unsafe artifact paths
- shell/subprocess argument injection
- unsafe handling of uploaded demos or filenames
- SQL construction and migration risks
- auth/authorization gaps in HTTP handlers
- overly broad filesystem permissions
- secrets or `.env` access
- unbounded memory/disk use from uploads, media, or generated artifacts
- dependency changes and vulnerable packages
- external tool invocation boundaries for FFmpeg, HLAE, CS2, Docker, PowerShell

Recommend whether to run:

- `scripts/go-gate.sh --security`
- `govulncheck ./...`
- `gosec ./...`

Use BLOCKER/WARNING/NIT. If clean, say `No blocking security issues found.`
