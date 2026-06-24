# Local end-to-end: upload a demo to an upload-ready reel

This runs the full FragForge pipeline on one machine and drives it from the
local HTMX workbench served by `zv-orchestrator`: drop a `.dem`, pick a player,
record locally through HLAE + CS2, and render an upload-ready pack.

No Postgres, Redis, Node, Next.js, or TypeScript dev server is required for this
local product flow. The orchestrator runs in memory mode with an inline task
queue and serves the UI at the same origin as the API.

## Prerequisites

- Go, matching `go.mod`.
- FFmpeg + ffprobe on `PATH`, or set `ZV_FFMPEG_PATH` and `ZV_FFPROBE_PATH`.
- HLAE 2.190+ and CS2 installed locally for recording:
  - `ZV_HLAE_PATH=C:/HLAE-2.190.1/HLAE.exe`
  - `ZV_CS2_PATH=C:/Program Files (x86)/Steam/steamapps/common/Counter-Strike Global Offensive/game/bin/win64/cs2.exe`

Use `C:\HLAE-2.190.1\HLAE.exe` on this machine. Do not use
`C:\HLAE\HLAE.exe`.

## Run It

```bash
scripts/run-local.sh
```

The script builds `zv-orchestrator`, `zv-recorder`, `zv-editor`, and
`zv-composer` into `./bin`, starts the orchestrator on `127.0.0.1:8080`, and
enables parse, record, compose, and render workers inline.

Open:

```text
http://127.0.0.1:8080/
```

Then:

1. Drop a `.dem`.
2. Either provide a target SteamID64 up front or let the roster scan finish and
   pick a player.
3. Parse the player.
4. Approve recording. CS2 is launched through HLAE on this PC.
5. Render with `viral-60-clean`, optional music, 9:16 or 16:9 format, kill
   effect, transition, intro, and outro.
6. Open the generated gallery or artifact links for `shortslistosparasubir`.

## Headless Backend Check

`scripts/e2e-local.sh` still drives the same orchestrator API headlessly and
asserts that a reel is produced. It launches CS2, so it is a local integration
check, not CI:

```bash
# with scripts/run-local.sh already running in another terminal
ZV_E2E_DEMO=/path/to/match.dem ZV_E2E_STEAMID=7656119... scripts/e2e-local.sh
```

## Stage Map

| Stage | Endpoint / binary | Output |
| --- | --- | --- |
| scan roster | `POST /api/jobs` without target -> `scan:roster` | `roster.json` |
| parse | `POST /api/jobs/{id}/parse` -> `parse:demo` | kill plan |
| record | `POST /api/jobs/{id}/record` -> `zv-recorder` via HLAE/CS2 | `segments/seg-NNN.mp4` |
| render | `POST /api/jobs/{id}/renders/viral-60-clean` -> `zv-editor` | `shortslistosparasubir` pack |

`compose` remains available for a 16:9 final MP4, but it is not required for the
reel workflow.
