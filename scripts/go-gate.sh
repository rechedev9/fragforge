#!/usr/bin/env bash
set -euo pipefail

race=false
security=false
build=false
format=true
staticcheck=true

usage() {
  cat >&2 <<'USAGE'
usage: scripts/go-gate.sh [--race] [--security] [--build] [--no-format] [--no-staticcheck]

Runs the repo Go quality gate.

Options:
  --race      run go test -race ./... -count=1
  --security  run govulncheck and gosec when installed
  --build     run go build ./cmd/...
  --no-format skip formatting changed Go files
  --no-staticcheck
              skip staticcheck (useful for the Windows fidelity gate after
              the platform-independent Ubuntu gate has already run it)
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --race) race=true ;;
    --security) security=true ;;
    --build) build=true ;;
    --no-format) format=false ;;
    --no-staticcheck) staticcheck=false ;;
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
go test ./... -count=1 -timeout "${ZV_GO_TEST_TIMEOUT:-3m}"

echo "== go vet =="
go vet ./...

echo "== zv check =="
go run ./cmd/zv check

if [ "$staticcheck" = true ]; then
  if staticcheck_path="$(go_tool_path staticcheck)"; then
    echo "== staticcheck =="
    "$staticcheck_path" ./...
  else
    echo "skip staticcheck: not installed"
  fi
fi

if [ "$build" = true ]; then
  echo "== go build ./cmd/... =="
  go build ./cmd/...
fi

if [ "$race" = true ]; then
  echo "== go test -race =="
  go test -race ./... -count=1 -timeout "${ZV_GO_RACE_TEST_TIMEOUT:-10m}"
fi

if [ "$security" = true ]; then
  if govulncheck_path="$(go_tool_path govulncheck)"; then
    echo "== govulncheck =="
    "$govulncheck_path" ./...
  else
    echo "skip govulncheck: not installed"
  fi

  if gosec_path="$(go_tool_path gosec)"; then
    echo "== gosec =="
    "$gosec_path" ./...
  else
    echo "skip gosec: not installed"
  fi
fi
