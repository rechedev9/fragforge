#!/usr/bin/env bash
# End-to-end test of the full reel pipeline WITHOUT CS2/HLAE, using the recorder's
# fake mode (ZV_RECORDER_FAKE=1, placeholder segment clips). Proves the real flow:
#   upload .dem -> scan roster -> parse (auto-picked top fragger) -> record (fake)
#   -> render (chosen preset variant + music) -> downloadable 1080x1920 reel.
#
# Self-contained: builds binaries, starts its own in-memory orchestrator on a
# private port, runs the flow, asserts the reel, and tears the orchestrator down.
# Requires Go, ffmpeg/ffprobe, and node (for JSON parsing). No CS2/HLAE.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PORT="${ZV_E2E_PORT:-8099}"
ADDR="127.0.0.1:$PORT"
ORCH="http://$ADDR"
VARIANT="${ZV_E2E_VARIANT:-clean-pov-60}"
MUSIC_KEY="${ZV_E2E_MUSIC:-edm-1}"
BIN="$ROOT/.local/e2e-bin"
DATA="$ROOT/.local/e2e-data"
WORK="$ROOT/.local/e2e-work"
MUSIC="${ZV_MUSIC_DIR:-$ROOT/.local/data/music}"
FFMPEG="${ZV_FFMPEG_PATH:-$(command -v ffmpeg || true)}"
FFPROBE="${ZV_FFPROBE_PATH:-$(command -v ffprobe || true)}"

DEMO="${ZV_E2E_DEMO:-}"
if [ -z "$DEMO" ]; then
  DEMO="$(ls "$ROOT"/.local/data/demos/*.dem 2>/dev/null | head -1 || true)"
fi
[ -n "$DEMO" ] && [ -f "$DEMO" ] || { echo "no demo found (set ZV_E2E_DEMO=/path.dem)" >&2; exit 1; }
[ -n "$FFPROBE" ] || { echo "ffprobe required (set ZV_FFPROBE_PATH)" >&2; exit 1; }

rm -rf "$DATA" "$WORK"
mkdir -p "$BIN" "$DATA" "$WORK"

echo "==> building binaries"
for c in zv-orchestrator zv-recorder zv-editor zv-composer; do
  go build -o "$BIN/$c.exe" "./cmd/$c"
done

echo "==> starting fake-mode orchestrator on $ADDR (demo=$(basename "$DEMO"))"
ZV_DATABASE_URL=memory ZV_HTTP_ADDR="$ADDR" \
  ZV_DATA_DIR="$DATA" ZV_MEDIA_WORK_DIR="$WORK" \
  ZV_RECORDER_FAKE=1 \
  ZV_RECORDER_PATH="$BIN/zv-recorder.exe" ZV_HLAE_PATH="fake-hlae" ZV_CS2_PATH="fake-cs2" \
  ZV_EDITOR_PATH="$BIN/zv-editor.exe" ZV_COMPOSER_PATH="$BIN/zv-composer.exe" \
  ZV_FFMPEG_PATH="$FFMPEG" ZV_FFPROBE_PATH="$FFPROBE" ZV_MUSIC_DIR="$MUSIC" \
  ZV_RECORD_TIMEOUT=5m ZV_RENDER_TIMEOUT=10m \
  "$BIN/zv-orchestrator.exe" >"$DATA/orch.log" 2>&1 &
ORCH_PID=$!
trap 'kill "$ORCH_PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 60); do curl -sf -o /dev/null "$ORCH/api/jobs" && break; sleep 1; done

# Tiny JSON field extractor via node (python3 is not available on this box).
jget() { node -e "let s='';process.stdin.on('data',d=>s+=d).on('end',()=>{try{let v=($2);console.log(v==null?'':v)}catch(e){console.log('')}})" "$1"; }
jstatus() { curl -s "$ORCH/api/jobs/$1" | jget _ "JSON.parse(s).status"; }
rstatus() { curl -s "$ORCH/api/jobs/$1/renders/$VARIANT" | jget _ "JSON.parse(s).status||'none'"; }
await() { # await <id> <fn> <target> <label> <max-tries>
  local id=$1 fn=$2 want=$3 label=$4 max=$5 s
  for ((i = 1; i <= max; i++)); do
    s="$($fn "$id")"
    echo "  $label[$i] ${s:-?}"
    [ "$s" = "$want" ] && return 0
    [ "$s" = "failed" ] && { echo "FAILED at $label"; tail -20 "$DATA/orch.log"; return 1; }
    sleep 2
  done
  echo "TIMEOUT waiting for $label=$want"
  tail -20 "$DATA/orch.log"
  return 1
}

echo "==> upload (scan path)"
JOB="$(curl -s -X POST "$ORCH/api/jobs" -F "demo=@$DEMO" | jget _ "JSON.parse(s).id")"
[ -n "$JOB" ] || { echo "no job id"; exit 1; }
echo "    job=$JOB"
await "$JOB" jstatus scanned scan 120

echo "==> auto-pick top fragger from roster"
STEAMID="$(curl -s "$ORCH/api/jobs/$JOB/roster" | jget _ "(()=>{const p=(JSON.parse(s).players||[]).slice().sort((a,b)=>(b.kills||0)-(a.kills||0));return p[0]&&p[0].steamid64})()")"
[ -n "$STEAMID" ] || { echo "no roster player"; exit 1; }
echo "    target=$STEAMID"

echo "==> parse (target=$STEAMID)"
curl -s -X POST "$ORCH/api/jobs/$JOB/parse" -H 'Content-Type: application/json' -d "{\"target_steamid\":\"$STEAMID\"}" >/dev/null
await "$JOB" jstatus parsed parse 240

echo "==> record (FAKE recorder, preset=$VARIANT)"
curl -s -X POST "$ORCH/api/jobs/$JOB/record" -H 'Content-Type: application/json' -d "{\"preset\":\"$VARIANT\"}" >/dev/null
await "$JOB" jstatus recorded record 120

echo "==> render $VARIANT (music=$MUSIC_KEY)"
curl -s -X POST "$ORCH/api/jobs/$JOB/renders/$VARIANT" -H 'Content-Type: application/json' -d "{\"music\":\"$MUSIC_KEY\"}" >/dev/null
await "$JOB" rstatus ready render 180

SEG="$(curl -s "$ORCH/api/jobs/$JOB/plan" | jget _ "(()=>{const seg=(JSON.parse(s).segments||[])[0];return seg&&seg.id})()")"
[ -n "$SEG" ] || { echo "no segment in plan"; exit 1; }
OUT="$(mktemp -u).mp4"
echo "==> download reel seg=$SEG"
curl -sf -o "$OUT" "$ORCH/api/jobs/$JOB/renders/$VARIANT/videos/$SEG"
W="$("$FFPROBE" -v error -select_streams v:0 -show_entries stream=width -of csv=p=0 "$OUT")"
H="$("$FFPROBE" -v error -select_streams v:0 -show_entries stream=height -of csv=p=0 "$OUT")"
BYTES="$(wc -c <"$OUT" | tr -d ' ')"
echo "    reel ${W}x${H} (${BYTES} bytes) at $OUT"
{ [ "$W" = "1080" ] && [ "$H" = "1920" ]; } || { echo "FAIL: reel is not 1080x1920"; exit 1; }
[ "${BYTES:-0}" -gt 1000 ] || { echo "FAIL: reel is empty"; exit 1; }
echo "PASS: vertical reel produced end-to-end (job=$JOB preset=$VARIANT music=$MUSIC_KEY seg=$SEG)"
