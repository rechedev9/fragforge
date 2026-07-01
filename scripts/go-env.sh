#!/usr/bin/env bash

ensure_go_toolchain() {
  if command -v go >/dev/null 2>&1 && command -v gofmt >/dev/null 2>&1; then
    return 0
  fi

  local candidates=(
    "/c/Program Files/Go/bin"
    "/c/Program Files (x86)/Go/bin"
    "/mnt/c/Program Files/Go/bin"
    "/mnt/c/Program Files (x86)/Go/bin"
  )

  local dir
  for dir in "${candidates[@]}"; do
    if [ -x "$dir/go" ] || [ -x "$dir/go.exe" ]; then
      export PATH="$dir:$PATH"
      break
    fi
  done

  if ! command -v go >/dev/null 2>&1 && command -v go.exe >/dev/null 2>&1; then
    go() {
      go.exe "$@"
    }
  fi
  if ! command -v gofmt >/dev/null 2>&1 && command -v gofmt.exe >/dev/null 2>&1; then
    gofmt() {
      gofmt.exe "$@"
    }
  fi

  if ! command -v go >/dev/null 2>&1; then
    echo "go not found: install Go or add it to PATH" >&2
    return 127
  fi
  if ! command -v gofmt >/dev/null 2>&1; then
    echo "gofmt not found: install Go or add it to PATH" >&2
    return 127
  fi
}
