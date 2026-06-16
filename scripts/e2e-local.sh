#!/usr/bin/env bash
# Repeatable end-to-end check of the local reel pipeline against a running
# orchestrator (start one with scripts/run-local.sh, or set ZV_E2E_ORCH).
#
# Drives the real backend the web UI uses: upload a .dem (scan path) -> roster
# -> parse (chosen player) -> record (HLAE/CS2) -> render (viral-60-clean), then
# asserts a 1080x1920 vertical reel mp4 is downloadable. Launches CS2, so this is
# a local integration test, not CI.
#
# Usage: ZV_E2E_DEMO=/path/to.dem ZV_E2E_STEAMID=765... scripts/e2e-local.sh
set -euo pipefail

ORCH="${ZV_E2E_ORCH:-http://127.0.0.1:8080}"
DEMO="${ZV_E2E_DEMO:?set ZV_E2E_DEMO to a .dem path}"
STEAMID="${ZV_E2E_STEAMID:?set ZV_E2E_STEAMID to the target SteamID64}"
VARIANT="${ZV_E2E_VARIANT:-viral-60-clean}"
FFPROBE="${ZV_FFPROBE_PATH:-ffprobe}"

jstatus() { curl -s "$ORCH/api/jobs/$1" | grep -o '"status":"[a-z]*"' | head -1 | sed 's/.*:"//;s/"//'; }
rstatus() { curl -s "$ORCH/api/jobs/$1/renders/$VARIANT" | grep -o '"status":"[a-z]*"' | head -1 | sed 's/.*:"//;s/"//'; }
await() { # await <jobId> <fn> <target> <label> <max-tries>
  local id=$1 fn=$2 want=$3 label=$4 max=$5 s
  for ((i = 1; i <= max; i++)); do
    s="$($fn "$id")"
    echo "  $label[$i] ${s:-?}"
    [ "$s" = "$want" ] && return 0
    [ "$s" = "failed" ] && { echo "FAILED at $label"; return 1; }
    sleep 3
  done
  echo "TIMEOUT waiting for $label=$want"; return 1
}

echo "==> upload (scan path, no target)"
JOB="$(curl -s -X POST "$ORCH/api/jobs" -F "demo=@$DEMO" | grep -o '"id":"[^"]*"' | sed 's/"id":"//;s/"//')"
[ -n "$JOB" ] || { echo "no job id"; exit 1; }
echo "    job=$JOB"

await "$JOB" jstatus scanned scan 200
echo "==> parse (target=$STEAMID)"
curl -s -X POST "$ORCH/api/jobs/$JOB/parse" -H 'Content-Type: application/json' -d "{\"target_steamid\":\"$STEAMID\"}" >/dev/null
await "$JOB" jstatus parsed parse 200

echo "==> record (HLAE/CS2)"
curl -s -X POST "$ORCH/api/jobs/$JOB/record" >/dev/null
await "$JOB" jstatus recorded record 400

echo "==> render $VARIANT"
curl -s -X POST "$ORCH/api/jobs/$JOB/renders/$VARIANT" >/dev/null
await "$JOB" rstatus ready render 400

SEG="$(curl -s "$ORCH/api/jobs/$JOB/plan" | grep -o '"id": *"seg-[0-9]*"' | head -1 | grep -o 'seg-[0-9]*')"
OUT="$(mktemp -u).mp4"
echo "==> download reel $SEG"
curl -sf -o "$OUT" "$ORCH/api/jobs/$JOB/renders/$VARIANT/videos/$SEG"
W="$("$FFPROBE" -v error -select_streams v:0 -show_entries stream=width -of csv=p=0 "$OUT")"
H="$("$FFPROBE" -v error -select_streams v:0 -show_entries stream=height -of csv=p=0 "$OUT")"
echo "    reel ${W}x${H} at $OUT"
[ "$W" = "1080" ] && [ "$H" = "1920" ] || { echo "FAIL: reel is not 1080x1920"; exit 1; }
echo "PASS: vertical reel produced end-to-end (job=$JOB seg=$SEG)"
