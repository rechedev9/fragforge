#!/usr/bin/env bash
# One-box local deploy of the full FragForge pipeline for the HTMX local UI:
#   upload .dem -> scan roster -> pick player -> parse -> record (HLAE/CS2)
#   -> render reel (viral-60-clean) -> upload-ready artifacts.
#
# BYO-PC: capture runs on THIS machine. Requires Go, FFmpeg, and — for the
# record stage — HLAE and CS2 (Steam) installed locally. No Postgres/Redis: the
# orchestrator runs in memory mode with an inline task queue.
#
# Tool paths are auto-detected; explicit environment overrides still win.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BIN="${ZV_BIN_DIR:-$ROOT/bin}"
DATA="${ZV_DATA_DIR:-$ROOT/.local/data}"
WORK="${ZV_MEDIA_WORK_DIR:-$ROOT/.local/work}"
HTTP_ADDR="${ZV_HTTP_ADDR:-127.0.0.1:8080}"

HLAE="${ZV_HLAE_PATH:-}"
CS2="${ZV_CS2_PATH:-C:/Program Files (x86)/Steam/steamapps/common/Counter-Strike Global Offensive/game/bin/win64/cs2.exe}"
FFMPEG="${ZV_FFMPEG_PATH:-$(command -v ffmpeg || true)}"
FFPROBE="${ZV_FFPROBE_PATH:-$(command -v ffprobe || true)}"

MUSIC="${ZV_MUSIC_DIR:-$DATA/music}"
mkdir -p "$BIN" "$DATA" "$WORK" "$MUSIC"

echo "==> building pipeline binaries into $BIN"
for c in zv-orchestrator zv-recorder zv-editor zv-composer; do
  go build -o "$BIN/$c.exe" "./cmd/$c"
done

# Provision the curated open-source music catalog (CC0 / CC-BY, see
# data/music/ATTRIBUTION.md) into $MUSIC so the UI song picker has real tracks.
# Idempotent: skips tracks already downloaded. The orchestrator's /api/songs
# reads $MUSIC/catalog.json for the metadata it serves the web app.
ZV_MUSIC_DIR="$MUSIC" ZV_FFPROBE_PATH="$FFPROBE" bash "$ROOT/scripts/fetch-music.sh" || \
  echo "==> music fetch incomplete (offline?); falling back to placeholder beds"

# Placeholder music beds (synthesized) as an offline fallback so Music Edit
# always has something to mix. With a real catalog present these do not appear
# in /api/songs. Drop real tracks named "<songId>.<ext>" into $MUSIC to add more.
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

echo "==> starting orchestrator + HTMX UI on http://$HTTP_ADDR (parse+record+compose+render, in-memory)"
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

echo "==> local UI ready: http://$HTTP_ADDR/"
echo "    drop a .dem, pick a player, record through HLAE/CS2, and render from the HTMX workbench."
wait "$ORCH_PID"
