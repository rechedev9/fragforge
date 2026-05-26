#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
export CLAUDE_MAX_TURNS="${CLAUDE_MAX_TURNS:-12}"
exec "$root/scripts/claude-run.sh" .claude/commands/zv-tdd.md "$@"
