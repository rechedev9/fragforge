# FragForge Studio (desktop)

A Windows desktop wrapper around Local Studio: one app that boots the Go
orchestrator and the Next.js web UI (in local mode) and shows the flow in a
native window, so an end user never touches Node, a terminal, or a browser.

It bundles the same pieces `scripts/local-studio.ps1` runs:

- `zv-orchestrator.exe` - spawned directly (not via `zv serve`), so quitting
  the app kills the real server instead of leaving an orphaned grandchild
  holding the port and the SQLite job db. Runs with `ZV_DATABASE_URL=sqlite`
  (job state persists in `<userData>/data/jobs.db` across restarts) and
  `ZV_DATA_DIR=<userData>/data`; HLAE/CS2/FFmpeg are auto-detected, or use the
  tools provisioned on first boot below.
- `zv-recorder.exe` and `zv-editor.exe` - the required capture and render
  workers, auto-detected beside the orchestrator.
- The Next.js standalone server - started with Electron's own Node (no separate
  Node runtime shipped), in local mode so the UI proxies the whole pipeline to
  the orchestrator.

Both processes bind loopback (`ZV_HTTP_ADDR=127.0.0.1:<port>`) on ports chosen
once per install and persisted in `<userData>/ports.json`; the web port in
particular must stay stable across launches because the reel library lives in
the browser's `localStorage`, which is keyed by origin (`host:port`).

On first boot the app provisions the official HLAE 2.191.0 release, FFmpeg, and
yt-dlp (~110 MB total) into `<userData>/tools`, each verified against a pinned
sha256 digest, plus the music catalog. Every download is best-effort, so an
offline first boot just leaves that feature unconfigured until the next launch.
The HLAE version is intentionally fixed instead of following the latest release
so every desktop build uses the same official package. The window
lands on `/matches` (the app shell/dashboard, not a single flow), since Studio
has both the demo-upload path and the Twitch stream-clips path.

Capture still needs CS2 installed on the machine (Windows + GPU); Studio installs
HLAE automatically. Job data (demos, artifacts) is written under the per-user
app data dir, not Program Files.

Finished Library reels include a manual publication assistant. Studio generates
Madrid-time guidance and factual metadata alternatives, lets the user copy the
title, description, and tags, downloads the MP4, and opens
`https://studio.youtube.com/` in the system browser. The user completes YouTube's
official **CREAR -> Subir vídeos** flow there, including channel, audience,
visibility, and scheduling choices. No Google credentials are required by the
installer. Optional public trend hints remain available when
`FIRECRAWL_API_KEY` is inherited by the desktop process.

The standard installer remains credential-free. FragForge also has an explicit
internal team build that packages one shared `XAI_API_KEY` for stream
subtitles. At runtime a locally configured `XAI_API_KEY` takes precedence, so
the shared key can be replaced immediately without rebuilding. The credential
is passed only to `zv-orchestrator.exe`, never to the bundled Next.js server or
the renderer.

## Build the installer (on Windows)

Prerequisites: Go 1.26+, Node.js + npm, and the web deps installed.

```powershell
# 1. From the repo root: build the Go binaries.
.\scripts\build.ps1

# 2. Install web deps (the assemble step runs the Next.js production build).
cd web; npm install; cd ..

# 3. Build the desktop app.
cd desktop
npm install
npm run dist
```

`npm run dist` runs `scripts/assemble.mjs` (builds the web in local mode and
stages `zv-orchestrator.exe`, `zv-editor.exe`, `zv-recorder.exe`, and the
standalone server into `build-resources/`), then `electron-builder` produces the
installer under `dist-installer/` (`FragForge Studio Setup <version>.exe`,
where `<version>` is the `version` field in `desktop/package.json`). The app
icon lives at `build/icon.ico`, which electron-builder picks up automatically;
`assemble.mjs` fails fast if it's missing. `zv-orchestrator.exe`,
`zv-editor.exe`, and `zv-recorder.exe` are required at assemble time so the
packaged app can parse, capture, and render reels. The developer `zv.exe` CLI
stays available in the repository build but is not shipped in the desktop
installer.

To produce the internal team installer, load the shared key without placing it
in command history and use the separate build target:

```powershell
$secureKey = Read-Host "xAI team API key" -AsSecureString
$env:XAI_API_KEY = (New-Object System.Net.NetworkCredential("", $secureKey)).Password
npm run dist:team
Remove-Item Env:XAI_API_KEY
```

`npm run dist:team` refuses to run when the key is missing or malformed. It
stores the credential in `resources/team/xai-api-key` inside the installed app;
the ignored `build-resources/` and `dist-installer/` directories are its only
local staging locations. This is convenience, not secret protection: anyone
who receives the installer can extract and use the shared key, so distribute
that edition only to people authorized to consume the team's xAI quota. Running
the ordinary `npm run dist` afterward replaces the staged key with an empty
resource and produces a credential-free installer.

This v2 is unsigned, so Windows SmartScreen shows an "unknown publisher" prompt
on first run - choose "More info" -> "Run anyway". Code signing and auto-update
are intentionally out of scope for v2.

## Run without packaging (dev)

```powershell
# From the repo root, once: build the Go binaries and the standalone bundle.
.\scripts\build.ps1
cd desktop; npm install
npm run assemble        # builds the web + stages build-resources/

# In dev, src/main.ts resolves every bundled resource (zv-orchestrator.exe,
# zv-editor.exe, zv-recorder.exe, the web server) from .\build-resources, the
# same layout `npm run assemble` stages for packaging. Launch the Electron shell:
npm start
```

## How it works

`src/main.ts` (Electron main process, compiled to `dist/main.js`):

1. Reads or picks two per-install-stable loopback ports (`orchestrator`,
   `web`), persisted in `<userData>/ports.json`.
2. Kicks off music catalog provisioning in the background, and awaits
   provisioning of HLAE/FFmpeg/yt-dlp into `<userData>/tools` (first boot
   only; later boots return the cached installs instantly).
3. Spawns `zv-orchestrator.exe` directly - without a `zv.exe serve`
   intermediary - so quitting the app reliably kills the real server (`ZV_DATABASE_URL=sqlite`,
   `ZV_DATA_DIR=<userData>/data`, `ZV_HTTP_ADDR=127.0.0.1:<orchPort>`, plus any
   provisioned tool paths).
4. Spawns the Next standalone `server.js` via `ELECTRON_RUN_AS_NODE`
   (`ORCHESTRATOR_URL` pointing at the orchestrator, `PORT=<webPort>`).
5. Waits for `/healthz` and the web root.
6. Loads `/matches` in the window.
7. Kills the orchestrator and web children on quit. A single-instance lock
   prevents a second launch from spawning a duplicate backend.
