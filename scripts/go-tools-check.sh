#!/usr/bin/env bash
set -euo pipefail

missing=0

if command -v go >/dev/null 2>&1; then
  go version
else
  echo "missing: go"
  missing=1
fi

for tool in goimports staticcheck govulncheck gosec; do
  if command -v "$tool" >/dev/null 2>&1; then
    printf 'found: %s -> %s\n' "$tool" "$(command -v "$tool")"
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

Make sure ~/go/bin is in PATH:
  export PATH="$HOME/go/bin:$PATH"
EOF
  exit 1
fi

echo "OK: Go quality tools available"
