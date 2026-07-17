#!/usr/bin/env bash
# Provision the curated open-source music catalog into ZV_MUSIC_DIR.
#
# Reads data/music/catalog.json (committed metadata: id, ext, downloadUrl,
# license) and downloads each track to "<ZV_MUSIC_DIR>/<id>.<ext>", skipping
# files already present. Idempotent and safe to re-run. Tracks are CC0 or CC-BY
# (see data/music/ATTRIBUTION.md). Requires curl and node (ffprobe optional).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CATALOG="$ROOT/data/music/catalog.json"
# Default to <repo>/data/music: the same directory the orchestrator serves when
# ZV_MUSIC_DIR is unset, so bare provisioning and a default `zv serve` agree.
DEST="${ZV_MUSIC_DIR:-$ROOT/data/music}"
FFPROBE="${ZV_FFPROBE_PATH:-$(command -v ffprobe || true)}"

if [ ! -f "$CATALOG" ]; then
  echo "music catalog not found: $CATALOG" >&2
  exit 1
fi

mkdir -p "$DEST"
# The orchestrator's /api/songs reads <ZV_MUSIC_DIR>/catalog.json for metadata.
cp "$CATALOG" "$DEST/catalog.json"

ok=0
fail=0
while IFS=$'\t' read -r id ext url; do
  [ -n "$id" ] || continue
  out="$DEST/$id.$ext"
  if [ -f "$out" ]; then
    echo "  have $id.$ext"
    ok=$((ok + 1))
    continue
  fi
  echo "  fetch $id <- $url"
  if ! curl -fsSL --retry 2 -o "$out" "$url"; then
    echo "  FAILED download $id" >&2
    rm -f "$out"
    fail=$((fail + 1))
    continue
  fi
  # Reject anything that is not real, long-enough audio.
  if [ -n "$FFPROBE" ]; then
    dur="$("$FFPROBE" -v error -show_entries format=duration -of default=nw=1:nk=1 "$out" 2>/dev/null || echo 0)"
    if ! awk -v d="${dur:-0}" 'BEGIN{exit !(d+0 >= 20)}'; then
      echo "  REJECT $id (duration=${dur:-?}s < 20s)" >&2
      rm -f "$out"
      fail=$((fail + 1))
      continue
    fi
  fi
  ok=$((ok + 1))
done < <(node -e '
const fs = require("fs");
const doc = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
for (const t of doc.tracks || []) {
  const id = t.id || "", ext = t.ext || "mp3", url = t.downloadUrl || "";
  if (id && url) process.stdout.write(`${id}\t${ext}\t${url}\n`);
}
' "$CATALOG")

echo "music ready in $DEST ($ok ok, $fail failed)"
