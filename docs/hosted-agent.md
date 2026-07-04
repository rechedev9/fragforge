# FragForge Agent (hosted mode)

The FragForge Agent is the local, headless companion for the hosted web app.

In hosted mode the FragForge web UI is served by our domain
(`https://fragforge.gr-prod.taila10698.ts.net`), but nothing heavy runs on our
server.
The browser on your PC loads the SPA from our domain and then talks DIRECTLY to
the Agent running on your machine at `http://127.0.0.1:8787`.
All jobs, uploads, recording, rendering and the final video files stay on your
PC and never transit our server.

The Agent IS the existing `zv-orchestrator` binary, wrapped by the same Electron
desktop layer as FragForge Studio but run in a headless "agent-only" mode.
It provisions the capture/render tools (HLAE, FFmpeg, yt-dlp) on first run and
exposes the local API the hosted SPA calls.

## What runs where

- Our server: the SPA (static) plus a small SQLite accounts DB (Steam login).
  No video, no jobs, no heavy compute.
- Your PC (the Agent): the orchestrator, the job queue, CS2/HLAE capture,
  rendering, and every media file.

## Security model (why this is safe)

The Agent binds loopback only (`127.0.0.1:8787`), so it is not reachable from
the network.
Because a page served from our HTTPS domain is a CROSS-SITE origin relative to
the loopback Agent, two protections apply:

1. Pairing token.
   On first run the Agent generates a random 32-byte token (base64url, no
   padding) and persists it at `<DataDir>/agent-pairing.token` with `0600`
   permissions.
   The browser stores it in `localStorage` and sends it on EVERY request in the
   `X-FragForge-Token` header.
   Any cross-site request (which is exactly what the hosted SPA makes) MUST
   carry a valid token for both reads and mutations, even on loopback.
   If you set `ZV_MUTATION_TOKEN`, that value is used as the pairing token
   instead of a generated one.

2. CORS + Private Network Access.
   The Agent only sends CORS headers to Origins listed in
   `ZV_ALLOWED_WEB_ORIGINS`.
   The Agent installer defaults this to our hosted domain, so an arbitrary web
   page cannot read from or drive your Agent.

Same-origin and non-browser clients (the CLI, FragForge Studio's own loopback
UI, server-to-server) are unaffected and keep working with no token.

## Install and run on Windows

1. Download and run the FragForge Agent installer (`FragForge Agent Setup
   x.y.z.exe`).
   This v1 is unsigned, so Windows SmartScreen shows an "unknown publisher"
   prompt on first run - choose "More info" then "Run anyway".

2. Launch FragForge Agent.
   It runs in the background with a system-tray icon (no window).
   On first run it downloads the runtime tools it needs (HLAE, FFmpeg, yt-dlp;
   roughly 110 MB total); later launches are instant.
   CS2 and HLAE capture still require CS2 installed on the PC with a GPU - the
   Agent only removes the setup friction, not that requirement.

3. Read your pairing token.
   Right-click the tray icon and choose "Copiar token de emparejamiento" to copy
   the token, and "Copiar URL del agente" to copy the URL.
   The token and the URL are also written to the log (tray menu: "Abrir registro
   (studio.log)") and the token is stored at
   `%APPDATA%\FragForge Agent\data\agent-pairing.token`.

## Pair the Agent with the hosted web

1. Open the hosted web app and sign in with Steam.
2. Open the "Conexion con el agente" (Agent connection) panel.
3. Confirm the Agent URL is `http://127.0.0.1:8787` (change it only if you
   overrode the port).
4. Paste the pairing token and save.
   The panel probes the Agent (`GET /healthz`) and shows connected once the
   token is accepted.

From then on every job - upload, record, render, download - runs on your PC via
the Agent, and the final MP4 is fetched by the browser straight from the Agent.
Bytes never touch our server.

## Configuration

The Agent reads the same environment variables as the orchestrator.
The ones that matter for hosted mode:

- `FRAGFORGE_AGENT_ONLY=1` - force headless agent mode (the dedicated Agent
  installer sets this implicitly via a bundled `agent-mode.flag` marker; you
  only need it when running the Studio build headless).
- `FRAGFORGE_AGENT_ADDR` - override the bind address (default `127.0.0.1:8787`).
- `ZV_ALLOWED_WEB_ORIGINS` - comma-separated exact web origins allowed to call
  the Agent (default: our hosted domain).
  This is the single "hosted mode is on" signal; a non-empty value is what makes
  the Agent generate, persist and require the pairing token.
- `ZV_MUTATION_TOKEN` - use a fixed pairing token instead of a generated one.

## Building the installer (Windows only)

The Agent installer can only be built on a Windows host - it packages Windows
`.exe` binaries and produces an NSIS installer.
It cannot be built or E2E-tested on this Linux box.

```powershell
# 1. From the repo root: build the Go binaries (produces .\bin\zv*.exe).
.\scripts\build.ps1

# 2. Build the Agent installer (no web build needed - the web is hosted).
cd desktop
npm install
npm run dist:agent
```

`npm run dist:agent` runs `scripts/assemble-agent.mjs` (stages the Go binaries,
the music catalog and the `agent-mode.flag` marker into `build-resources/`),
then `electron-builder --config electron-builder-agent.yml` produces the
installer under `desktop/dist-agent/`.

Producing a SIGNED `.exe` (to avoid the SmartScreen "unknown publisher" prompt)
additionally requires a Windows build host with a code signing certificate.
Code signing and auto-update are out of scope for v1.

## Run headless without packaging (dev)

```powershell
# From the repo root, once: build the Go binaries.
.\scripts\build.ps1

cd desktop
npm install
npm run assemble:agent   # stages bin + music + agent-mode.flag into build-resources/
npm start                # main.js sees the marker and boots agentBoot()
```

The pairing token and Agent URL are printed to the console (and mirrored to
`studio.log`), and the tray icon exposes both.
