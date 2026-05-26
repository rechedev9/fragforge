#!/usr/bin/env bash
set -euo pipefail

race=false
security=false
build=false
format=true

usage() {
  cat >&2 <<'USAGE'
usage: scripts/go-gate.sh [--race] [--security] [--build] [--no-format]

Runs the repo Go quality gate.

Options:
  --race      run go test -race ./... -count=1
  --security  run govulncheck and gosec when installed
  --build     run go build ./cmd/...
  --no-format skip formatting changed Go files
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --race) race=true ;;
    --security) security=true ;;
    --build) build=true ;;
    --no-format) format=false ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; usage; exit 2 ;;
  esac
done

root="$(git rev-parse --show-toplevel)"
cd "$root"

source scripts/go-env.sh
ensure_go_toolchain

if [ "$format" = true ]; then
  echo "== format changed Go files =="
  scripts/go-format-changed.sh
fi

echo "== go test =="
go test ./... -count=1

echo "== go vet =="
go vet ./...

echo "== zv check =="
go run ./cmd/zv check

if command -v staticcheck >/dev/null 2>&1; then
  echo "== staticcheck =="
  staticcheck ./...
else
  echo "skip staticcheck: not installed"
fi

if [ "$build" = true ]; then
  echo "== go build ./cmd/... =="
  go build ./cmd/...
fi

if [ "$race" = true ]; then
  echo "== go test -race =="
  go test -race ./... -count=1
fi

if [ "$security" = true ]; then
  if command -v govulncheck >/dev/null 2>&1; then
    echo "== govulncheck =="
    govulncheck ./...
  else
    echo "skip govulncheck: not installed"
  fi

  if command -v gosec >/dev/null 2>&1; then
    echo "== gosec =="
    gosec ./...
  else
    echo "skip gosec: not installed"
  fi
fi
