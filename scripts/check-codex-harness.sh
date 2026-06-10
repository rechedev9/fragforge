#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

source scripts/go-env.sh
ensure_go_toolchain

if ! command -v codex >/dev/null 2>&1; then
  echo "FAIL: codex CLI not found" >&2
  echo "Install with: npm install -g @openai/codex" >&2
  exit 1
fi

echo "== shell syntax =="
mapfile -t shell_scripts < <(find scripts -maxdepth 1 -type f -name '*.sh' | sort)
bash -n "${shell_scripts[@]}"

echo "== Codex sees AGENTS.md =="
tmp="$(mktemp)"
err="$(mktemp)"
trap 'rm -f "$tmp" "$err"' EXIT
if ! codex --cd "$root" debug prompt-input "harness smoke test" > "$tmp" 2> "$err"; then
  echo "FAIL: codex CLI could not read project prompt context" >&2
  if grep -q "@openai/codex-linux-x64" "$err"; then
    echo "Missing optional dependency @openai/codex-linux-x64. Reinstall with: npm install -g @openai/codex@latest" >&2
  else
    cat "$err" >&2
  fi
  exit 1
fi

grep -q "FragForge is a deterministic CS2 demo-to-video pipeline" "$tmp"
grep -q "AGENTS.md" "$tmp"

echo "== FragForge workflow contract =="
go run ./cmd/zv check

echo "OK: Codex harness is wired"
