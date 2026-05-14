#!/usr/bin/env bash
set -euo pipefail

DEMO="${1:-testdata/lavked-vs-tnc-m2-nuke.dem}"
TARGET="${2:-76561198148986856}"
BASE="${ZV_BASE_URL:-http://localhost:8080}"

if [ ! -f "$DEMO" ]; then
  echo "demo not found: $DEMO" >&2
  exit 1
fi

echo "→ uploading $DEMO with target=$TARGET"
JOB=$(curl -fsS -X POST "$BASE/api/jobs" \
  -F "demo=@$DEMO" \
  -F "config={\"target_steamid\":\"$TARGET\"}" | tee /dev/stderr)
ID=$(echo "$JOB" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")

echo "→ job id = $ID; polling status…"
for i in $(seq 1 60); do
  STATUS=$(curl -fsS "$BASE/api/jobs/$ID" | python3 -c "import sys,json;print(json.load(sys.stdin)['status'])")
  echo "  [$i] status=$STATUS"
  case "$STATUS" in
    parsed) break ;;
    failed) echo "job failed" >&2; exit 2 ;;
  esac
  sleep 2
done

if [ "$STATUS" != "parsed" ]; then
  echo "timeout waiting for parse" >&2
  exit 3
fi

echo "→ fetching plan"
curl -fsS "$BASE/api/jobs/$ID/plan" | python3 -m json.tool | head -40
echo "✔ smoke test passed"
