# FragForge Studio (desktop)

A Windows desktop wrapper around Local Studio: one app that boots the Go
orchestrator and the Next.js web UI (in local mode) and shows the flow in a
native window, so an end user never touches Node, a terminal, or a browser.

It bundles the same pieces `scripts/local-studio.ps1` runs:

- `zv.exe` (the orchestrator) - started in memory mode, HLAE/CS2 auto-detected.
- The Next.js standalone server - started with Electron's own Node (no separate
  Node runtime shipped), in local mode so the UI proxies the whole pipeline to
  the orchestrator.

Capture still needs CS2 + HLAE installed on the machine (Windows + GPU); the app
only removes the setup friction, not that requirement. Job data (demos,
artifacts) is written under the per-user app data dir, not Program Files.

## Build the installer (on Windows)

Prerequisites: Go 1.26+, Node.js + npm, and the web deps installed.

```powershell
# 1. From the repo root: build the Go binaries (produces .\bin\zv.exe).
.\scripts\build.ps1

# 2. Install web deps (the assemble step runs the Next.js production build).
cd web; npm install; cd ..

# 3. Build the desktop app.
cd desktop
npm install
npm run dist
```

`npm run dist` runs `scripts/assemble.mjs` (builds the web in local mode and
stages `zv.exe` + the standalone server into `build-resources/`), then
`electron-builder` produces the installer under `dist-installer/`
(`FragForge Studio Setup 0.1.0.exe`).

This v1 is unsigned, so Windows SmartScreen shows an "unknown publisher" prompt
on first run - choose "More info" -> "Run anyway". Code signing and auto-update
are intentionally out of scope for v1.

## Run without packaging (dev)

```powershell
# From the repo root, once: build zv.exe and the standalone bundle.
.\scripts\build.ps1
cd desktop; npm install
npm run assemble        # builds the web + stages build-resources/

# In dev, main.js resolves zv.exe from ..\bin and the server from
# ..\web\.next\standalone. Launch the Electron shell:
npm start
```

## How it works

`main.js` (Electron main process):

1. Picks two free loopback ports.
2. Spawns `zv.exe serve` (`ZV_DATABASE_URL=memory`, `ZV_DATA_DIR=<userData>/data`,
   `ZV_HTTP_ADDR=127.0.0.1:<orchPort>`).
3. Spawns the Next standalone `server.js` via `ELECTRON_RUN_AS_NODE`
   (`NEXT_PUBLIC_FRAGFORGE_MODE=local`, `ORCHESTRATOR_URL` pointing at the
   orchestrator, `PORT=<webPort>`).
4. Waits for `/healthz` and the web root, then loads `/upload` in the window.
5. Kills both children on quit. A single-instance lock prevents a second launch
   from spawning a duplicate backend.
