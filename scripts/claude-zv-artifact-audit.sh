#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
export CLAUDE_ALLOWED_TOOLS="${CLAUDE_ALLOWED_TOOLS:-Read,Bash,WebSearch,WebFetch}"
export CLAUDE_MAX_TURNS="${CLAUDE_MAX_TURNS:-8}"
exec "$root/scripts/claude-run.sh" .claude/commands/zv-artifact-audit.md "$@"
