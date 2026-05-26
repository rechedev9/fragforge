#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
export CODEX_SANDBOX="${CODEX_SANDBOX:-read-only}"
export CODEX_APPROVAL="${CODEX_APPROVAL:-never}"
exec "$root/scripts/codex-run.sh" .codex/prompts/go-security-review.md "$@"
