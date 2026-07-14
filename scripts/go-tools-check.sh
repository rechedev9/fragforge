#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
source scripts/go-env.sh
ensure_go_toolchain

missing=0

go version

for tool in goimports staticcheck govulncheck gosec; do
  if tool_path="$(go_tool_path "$tool")"; then
    printf 'found: %s -> %s\n' "$tool" "$tool_path"
  else
    echo "missing: $tool"
    missing=1
  fi
done

if [ "$missing" -ne 0 ]; then
  cat <<'EOF'

Install optional Go quality tools:
  go install golang.org/x/tools/cmd/goimports@latest
  go install honnef.co/go/tools/cmd/staticcheck@latest
  go install golang.org/x/vuln/cmd/govulncheck@latest
  go install github.com/securego/gosec/v2/cmd/gosec@latest

The repository scripts also discover tools from `go env GOBIN` or
`go env GOPATH` when a Windows Go toolchain is called from Bash/WSL.
EOF
  exit 1
fi

echo "OK: Go quality tools available"
