#!/usr/bin/env bash
# One-box local deploy of the full FragForge pipeline for the web UI:
#   upload .dem -> scan roster -> pick player -> parse -> record (HLAE/CS2)
#   -> render vertical reel (viral-60-clean) -> playable/downloadable in /videos.
#
# BYO-PC: capture runs on THIS machine. Requires Go, Node, FFmpeg, and — for the
# record stage — HLAE and CS2 (Steam) installed locally. No Postgres/Redis: the
# orchestrator runs in memory mode with an inline task queue.
#
# Tool paths default to this dev machine (see CLAUDE.md) and are env-overridable.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BIN="${ZV_BIN_DIR:-$ROOT/bin}"
DATA="${ZV_DATA_DIR:-$ROOT/.local/data}"
WORK="${ZV_MEDIA_WORK_DIR:-$ROOT/.local/work}"
WEB_PORT="${ZV_WEB_PORT:-3300}"
HTTP_ADDR="${ZV_HTTP_ADDR:-127.0.0.1:8080}"

HLAE="${ZV_HLAE_PATH:-C:/HLAE-2.190.1/HLAE.exe}"
CS2="${ZV_CS2_PATH:-C:/Program Files (x86)/Steam/steamapps/common/Counter-Strike Global Offensive/game/bin/win64/cs2.exe}"
FFMPEG="${ZV_FFMPEG_PATH:-$(command -v ffmpeg || true)}"
FFPROBE="${ZV_FFPROBE_PATH:-$(command -v ffprobe || true)}"

MUSIC="${ZV_MUSIC_DIR:-$DATA/music}"
mkdir -p "$BIN" "$DATA" "$WORK" "$MUSIC"

echo "==> building pipeline binaries into $BIN"
for c in zv-orchestrator zv-recorder zv-editor zv-composer; do
  go build -o "$BIN/$c.exe" "./cmd/$c"
done

# Placeholder music beds (synthesized) so Music Edit has a track to mix. These
# are stand-ins, not licensed music — drop real tracks named "<songId>.m4a"
# (or .mp3/.ogg/.wav) into $MUSIC to override. Song ids match web fixtures.
if [ -n "$FFMPEG" ]; then
  gen_bed() { # <songId> <freqHz> <beatHz>
    for ext in m4a mp3 ogg opus wav aac; do [ -f "$MUSIC/$1.$ext" ] && return 0; done
    "$FFMPEG" -y -f lavfi -i "sine=frequency=$2:duration=30" \
      -af "tremolo=f=$3:d=0.8,aformat=channel_layouts=stereo,volume=0.5" \
      -c:a aac -b:a 128k "$MUSIC/$1.m4a" >/dev/null 2>&1 || true
  }
  gen_bed song-tikitaka-1 110 2
  gen_bed song-tikitaka-2 146 2.5
  gen_bed song-zerokull-1 98 1.75
  gen_bed song-zerokull-2 130 2.25
fi

# Sample reel served same-origin by the web app (web/public) so mock "ready" and
# feed reels are playable/downloadable in the demo with no external dependency.
# Real pipeline reels stream from the orchestrator instead.
SAMPLE="$ROOT/web/public/sample-reel.mp4"
if [ -n "$FFMPEG" ] && [ ! -f "$SAMPLE" ]; then
  "$FFMPEG" -y -f lavfi -i "testsrc=size=1080x1920:rate=30:duration=6" \
    -f lavfi -i "sine=frequency=300:duration=6" \
    -c:v libx264 -pix_fmt yuv420p -crf 28 -preset veryfast -c:a aac -shortest "$SAMPLE" >/dev/null 2>&1 || true
fi

echo "==> starting orchestrator on $HTTP_ADDR (parse+record+compose+render, in-memory)"
ZV_DATABASE_URL=memory ZV_HTTP_ADDR="$HTTP_ADDR" \
  ZV_DATA_DIR="$DATA" ZV_MEDIA_WORK_DIR="$WORK" \
  ZV_RECORDER_PATH="$BIN/zv-recorder.exe" ZV_HLAE_PATH="$HLAE" ZV_CS2_PATH="$CS2" \
  ZV_EDITOR_PATH="$BIN/zv-editor.exe" ZV_COMPOSER_PATH="$BIN/zv-composer.exe" \
  ZV_FFMPEG_PATH="$FFMPEG" ZV_FFPROBE_PATH="$FFPROBE" ZV_MUSIC_DIR="$MUSIC" \
  ZV_RECORD_TIMEOUT="${ZV_RECORD_TIMEOUT:-15m}" ZV_RENDER_TIMEOUT="${ZV_RENDER_TIMEOUT:-15m}" \
  "$BIN/zv-orchestrator.exe" &
ORCH_PID=$!
trap 'kill "$ORCH_PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 60); do
  curl -sf -o /dev/null "http://$HTTP_ADDR/api/jobs" && break
  sleep 1
done

echo "==> orchestrator up. starting web UI on http://localhost:$WEB_PORT (real API)"
echo "    open http://localhost:$WEB_PORT/upload and drop a .dem to run the full flow."
cd web
NEXT_PUBLIC_API_BASE=/api ORCHESTRATOR_URL="http://$HTTP_ADDR" npm run dev -- -p "$WEB_PORT"
