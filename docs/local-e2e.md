# Local end-to-end: upload a demo → vertical reel in the web UI

This runs the **entire** FragForge pipeline on one machine and drives it from the
web UI: drop a `.dem`, pick a player, and get a finished vertical (9:16) reel you
can play and download in the Library — recorded on your own rig with HLAE + CS2.

It is the BYO-PC model end to end. No Postgres or Redis: the orchestrator runs in
memory mode with an inline task queue.

## Prerequisites (this machine)

- **Go** and **Node** (see `go.mod` / `web/package.json`).
- **FFmpeg + ffprobe** on `PATH` (compose + render).
- **HLAE 2.190+** and **CS2** (Steam) for the record stage. Defaults:
  - `ZV_HLAE_PATH=C:/HLAE-2.190.1/HLAE.exe`
  - `ZV_CS2_PATH=C:/Program Files (x86)/Steam/steamapps/common/Counter-Strike Global Offensive/game/bin/win64/cs2.exe`
  - Override either via env. (Do **not** use `C:\HLAE\HLAE.exe`; that is the wrong install.)

## Run it

```bash
scripts/run-local.sh
```

This builds `zv-orchestrator`, `zv-recorder`, `zv-editor`, `zv-composer` into
`./bin`, starts the orchestrator on `127.0.0.1:8080` with **all** workers
enabled (parse, record, compose, render), and starts the web UI on
`http://localhost:3300` with the real API client (`NEXT_PUBLIC_API_BASE=/api`).
Stopping the web server (Ctrl+C) also stops the orchestrator.

Then in the browser:

1. Open `http://localhost:3300/upload` and drop a `.dem` (yours or anyone's).
2. The orchestrator scans the roster; pick the player to clip.
3. It parses that player's highlights → `/matches/<jobId>`.
4. Pick a highlight + **Clean POV** → **Create reel**. You land on `/videos`.
5. The reel advances **Queued → Capturing → Editing → Ready** (CS2 launches and
   records each segment; the editor renders the 9:16 short). Recording a full
   match takes a few minutes — the card shows a live "LIVE ON YOUR RIG" indicator.
6. When **Ready**, View/Download plays the real vertical reel.

The web layer stays 100% mock unless `NEXT_PUBLIC_API_BASE` is set, so the
default `npm run dev` is unaffected.

### Clean POV vs Music Edit

The recorder captures a **HUD-less POV with the deathnotices killfeed** (no
scoreboard) — that is the "clean POV". Override the capture HUD with
`ZV_RECORD_HUD` (`gameplay` | `clean` | `deathnotices`, default `deathnotices`).

**Music Edit** mixes a track into the reel. `run-local.sh` synthesizes
placeholder beds into `$ZV_MUSIC_DIR` (default `.local/data/music`) named after
the web song ids (`song-tikitaka-1.m4a`, …). Drop **real** tracks named
`<songId>.<ext>` (`.m4a/.mp3/.ogg/.wav`) into that directory to use them. When
the user picks Music Edit + a track, the render passes `--music <file>` to
`zv-editor`, which ducks the game audio under the track. Clean POV renders with
no music. (The placeholder beds are synthesized tones, not licensed music.)

## Repeatable backend check

`scripts/e2e-local.sh` drives the same orchestrator API headlessly and asserts a
`1080x1920` reel is produced (it launches CS2, so it is a local integration
check, not CI):

```bash
# with scripts/run-local.sh already running in another terminal
ZV_E2E_DEMO=/path/to/match.dem ZV_E2E_STEAMID=7656119... scripts/e2e-local.sh
```

## How the stages map

| Stage | Binary / endpoint | Output |
| --- | --- | --- |
| scan roster | `POST /api/jobs` (no target) → `scan:roster` | `roster.json` (players + K/D/A) |
| parse | `POST /api/jobs/{id}/parse` → `parse:demo` | kill plan (segments) |
| record | `POST /api/jobs/{id}/record` → `zv-recorder` (HLAE/CS2) | `segments/seg-NNN.mp4` (1080p60) |
| render | `POST /api/jobs/{id}/renders/viral-60-clean` → `zv-editor` | `renders/viral-60-clean/videos/seg-NNN.mp4` (1080x1920) |

`compose` (`POST /api/jobs/{id}/compose` → `zv-composer`) produces a 16:9
`final.mp4` and is **not** required for the vertical reel — the editor renders
the short directly from the recorded segments.
