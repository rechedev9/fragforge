#!/usr/bin/env bash
# Provision the curated open-source music catalog into ZV_MUSIC_DIR.
#
# Remote catalog tracks must supply a SHA-256. Existing cache entries are
# reverified; downloads are validated in a temporary file and atomically
# renamed into the library only after every check succeeds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CATALOG="$ROOT/data/music/catalog.json"
DEST="${ZV_MUSIC_DIR:-$ROOT/data/music}"
FFPROBE="${ZV_FFPROBE_PATH:-$(command -v ffprobe || true)}"

if [ ! -f "$CATALOG" ]; then
  echo "music catalog not found: $CATALOG" >&2
  exit 1
fi

hash_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  echo "sha256sum or shasum is required" >&2
  return 1
}

mkdir -p "$DEST"
# The orchestrator's /api/songs reads <ZV_MUSIC_DIR>/catalog.json for metadata.
if [ ! "$CATALOG" -ef "$DEST/catalog.json" ]; then
  cp "$CATALOG" "$DEST/catalog.json"
fi

ok=0
fail=0
while IFS=$'\t' read -r id ext url expected_sha; do
  [ -n "$id" ] || continue
  out="$DEST/$id.$ext"
  if ! [[ "$expected_sha" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo "  REJECT $id (missing or invalid sha256)" >&2
    rm -f -- "$out"
    fail=$((fail + 1))
    continue
  fi

  if [ -f "$out" ]; then
    cached_sha="$(hash_file "$out")" || exit 1
    if [ "${cached_sha,,}" = "${expected_sha,,}" ]; then
      echo "  have $id.$ext (sha256 verified)"
      ok=$((ok + 1))
      continue
    fi
    echo "  discard $id.$ext (sha256 mismatch)" >&2
    rm -f -- "$out"
  fi

  tmp="$(mktemp "$DEST/.${id}.${ext}.tmp.XXXXXX")"
  echo "  fetch $id <- $url"
  if ! curl -fsSL --proto '=https' --proto-redir '=https' --retry 2 -o "$tmp" "$url"; then
    echo "  FAILED download $id" >&2
    rm -f -- "$tmp"
    fail=$((fail + 1))
    continue
  fi

  downloaded_sha="$(hash_file "$tmp")" || { rm -f -- "$tmp"; exit 1; }
  if [ "${downloaded_sha,,}" != "${expected_sha,,}" ]; then
    echo "  REJECT $id (sha256 mismatch)" >&2
    rm -f -- "$tmp"
    fail=$((fail + 1))
    continue
  fi

  # Reject anything that is not real, long-enough audio before it reaches the
  # final destination; ffprobe/FFmpeg never process an unverified file.
  if [ -n "$FFPROBE" ]; then
    dur="$("$FFPROBE" -v error -show_entries format=duration -of default=nw=1:nk=1 "$tmp" 2>/dev/null || echo 0)"
    if ! awk -v d="${dur:-0}" 'BEGIN{exit !(d+0 >= 20)}'; then
      echo "  REJECT $id (duration=${dur:-?}s < 20s)" >&2
      rm -f -- "$tmp"
      fail=$((fail + 1))
      continue
    fi
  fi

  mv -f -- "$tmp" "$out"
  ok=$((ok + 1))
done < <(node -e '
const fs = require("fs");
const doc = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
for (const track of doc.tracks || []) {
  const id = track.id || "";
  const ext = track.ext || "mp3";
  const url = track.downloadUrl || "";
  const sha = track.sha256 || "";
  if (/^[a-z0-9][a-z0-9_-]*$/i.test(id) && /^[a-z0-9]+$/i.test(ext) && typeof url === "string" && url) {
    process.stdout.write(`${id}\t${ext}\t${url}\t${sha}\n`);
  }
}
' "$CATALOG")

echo "music ready in $DEST ($ok ok, $fail failed)"
