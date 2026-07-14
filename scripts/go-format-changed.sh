#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

source scripts/go-env.sh
ensure_go_toolchain

files=()

if [ "$#" -gt 0 ]; then
  for file in "$@"; do
    if [[ "$file" == *.go && -f "$file" ]]; then
      files+=("$file")
    fi
  done
else
  while IFS= read -r file; do
    if [[ "$file" == *.go && -f "$file" ]]; then
      files+=("$file")
    fi
  done < <(git ls-files --modified --others --exclude-standard -- '*.go')
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "no Go files to format"
  exit 0
fi

if goimports_path="$(go_tool_path goimports)"; then
  "$goimports_path" -w "${files[@]}"
else
  gofmt -w "${files[@]}"
fi

echo "formatted ${#files[@]} Go file(s)"
