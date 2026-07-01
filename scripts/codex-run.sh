#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/codex-run.sh <prompt-file> [task text...]

Reads a prompt playbook, appends optional task text and optional stdin, then
runs `codex exec` from the repository root.

Environment:
  CODEX_SANDBOX=workspace-write|read-only|danger-full-access
  CODEX_APPROVAL=on-request|never|untrusted
  CODEX_MODEL=<model>
  CODEX_PROFILE=<profile-from-~/.codex/config.toml>
  CODEX_SEARCH=1                  enable Codex web search
  CODEX_EPHEMERAL=1               do not persist session files
  CODEX_OUTPUT_LAST_MESSAGE=path  save Codex final message
  CODEX_DRY_RUN=1                 print command + final prompt, do not run

Examples:
  scripts/codex-run.sh .codex/prompts/go-tdd.md "add validation for ..."
  CODEX_SANDBOX=read-only scripts/codex-run.sh .codex/prompts/review-diff.md
  printf 'long task...' | scripts/codex-run.sh .codex/prompts/go-plan.md
USAGE
}

is_true() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|y|Y|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

if [ "$#" -lt 1 ]; then
  usage
  exit 2
fi

root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$root" ]; then
  echo "not inside a git repository; Codex project harness requires git" >&2
  exit 2
fi
cd "$root"

prompt_file="$1"
shift || true

if [ ! -f "$prompt_file" ]; then
  if [ -f "$root/$prompt_file" ]; then
    prompt_file="$root/$prompt_file"
  else
    echo "prompt file not found: $prompt_file" >&2
    exit 2
  fi
fi

sandbox="${CODEX_SANDBOX:-workspace-write}"
approval="${CODEX_APPROVAL:-on-request}"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

cat "$prompt_file" > "$tmp"

if [ "$#" -gt 0 ]; then
  {
    echo
    echo "## User task"
    printf '%s\n' "$*"
  } >> "$tmp"
fi

if [ ! -t 0 ]; then
  stdin_tmp="$(mktemp)"
  trap 'rm -f "$tmp" "$stdin_tmp"' EXIT
  cat > "$stdin_tmp"
  if [ -s "$stdin_tmp" ]; then
    {
      echo
      echo "## Stdin task/context"
      cat "$stdin_tmp"
    } >> "$tmp"
  fi
fi

global_args=(
  --cd "$root"
  --sandbox "$sandbox"
  --ask-for-approval "$approval"
)

exec_args=()

if [ -n "${CODEX_MODEL:-}" ]; then
  global_args+=(--model "$CODEX_MODEL")
fi

if [ -n "${CODEX_PROFILE:-}" ]; then
  global_args+=(--profile "$CODEX_PROFILE")
fi

if is_true "${CODEX_SEARCH:-}"; then
  global_args+=(--search)
fi

if is_true "${CODEX_EPHEMERAL:-}"; then
  exec_args+=(--ephemeral)
fi

if [ -n "${CODEX_OUTPUT_LAST_MESSAGE:-}" ]; then
  exec_args+=(--output-last-message "$CODEX_OUTPUT_LAST_MESSAGE")
fi

if is_true "${CODEX_DRY_RUN:-}"; then
  echo "repo: $root"
  echo "prompt: $prompt_file"
  echo "sandbox: $sandbox"
  echo "approval: $approval"
  printf 'command: codex'
  printf ' %q' "${global_args[@]}" exec "${exec_args[@]}" -
  echo
  echo "--- final prompt ---"
  cat "$tmp"
  exit 0
fi

if ! command -v codex >/dev/null 2>&1; then
  echo "codex CLI not found. Install with: npm install -g @openai/codex" >&2
  exit 127
fi

exec codex "${global_args[@]}" exec "${exec_args[@]}" - < "$tmp"
