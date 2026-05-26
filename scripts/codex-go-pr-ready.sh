#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
exec "$root/scripts/codex-run.sh" .codex/prompts/go-pr-ready.md "$@"
