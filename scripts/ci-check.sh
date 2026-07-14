#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

source scripts/go-env.sh
ensure_go_toolchain

echo "== actionlint =="
shopt -s nullglob
workflows=(.github/workflows/*.yml .github/workflows/*.yaml)
if [ "${#workflows[@]}" -eq 0 ]; then
  echo "no GitHub Actions workflows found" >&2
  exit 1
fi
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 "${workflows[@]}"
