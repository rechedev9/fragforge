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

go_tool_path() {
  local name="$1"
  local found
  found="$(command -v "$name" 2>/dev/null || command -v "$name.exe" 2>/dev/null || true)"
  if [ -n "$found" ]; then
    printf '%s\n' "$found"
    return 0
  fi

  local bin_dir
  bin_dir="$(go env GOBIN)"
  if [ -z "$bin_dir" ]; then
    bin_dir="$(go env GOPATH)/bin"
  fi
  case "$bin_dir" in
    [A-Za-z]:\\*)
      if command -v wslpath >/dev/null 2>&1; then
        bin_dir="$(wslpath -u "$bin_dir")"
      fi
      ;;
  esac
  for found in "$bin_dir/$name" "$bin_dir/$name.exe"; do
    if [ -x "$found" ]; then
      printf '%s\n' "$found"
      return 0
    fi
  done
  return 1
}
