#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

if [ "$#" -lt 1 ]; then
  echo "usage: scripts/claude-run.sh <prompt-file> [task text...]" >&2
  exit 2
fi

prompt_file="$1"
shift || true

if [[ "$prompt_file" != /* ]]; then
  prompt_file="$root/$prompt_file"
fi

if [ ! -f "$prompt_file" ]; then
  echo "prompt file not found: $prompt_file" >&2
  exit 2
fi

task="$*"
prompt="$(cat "$prompt_file")"
if [ -n "$task" ]; then
  prompt="${prompt}

User task:
${task}"
fi

if [ ! -t 0 ]; then
  stdin_tmp="$(mktemp)"
  trap 'rm -f "$stdin_tmp"' EXIT
  cat > "$stdin_tmp"
  if [ -s "$stdin_tmp" ]; then
    prompt="${prompt}

## Stdin task/context
$(cat "$stdin_tmp")"
  fi
fi

if [ "${CLAUDE_DRY_RUN:-0}" = "1" ]; then
  printf '%s\n' "$prompt"
  exit 0
fi

max_turns="${CLAUDE_MAX_TURNS:-12}"
allowed_tools="${CLAUDE_ALLOWED_TOOLS:-Read,Edit,Write,Bash,WebSearch,WebFetch}"

args=(-p "$prompt" --max-turns "$max_turns")

if [ -n "$allowed_tools" ]; then
  args+=(--allowedTools "$allowed_tools")
fi

if [ -n "${CLAUDE_MODEL:-}" ]; then
  args+=(--model "$CLAUDE_MODEL")
fi

if [ -n "${CLAUDE_OUTPUT_FORMAT:-}" ]; then
  args+=(--output-format "$CLAUDE_OUTPUT_FORMAT")
fi

exec claude "${args[@]}"
