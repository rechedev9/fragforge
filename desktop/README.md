# FragForge Studio (desktop)

A Windows desktop wrapper around Local Studio: one app that boots the Go
orchestrator and the Next.js web UI (in local mode) and shows the flow in a
native window, so an end user never touches Node, a terminal, or a browser.

Since 0.3.0, Studio is also a FragForge Cloud capture agent: once the user
pairs the app with their FragForge Cloud account, it runs `zv-agent.exe`
alongside the local orchestrator/web pair, so the hosted web
(`https://fragforge.167-233-55-246.sslip.io`) can drive this PC's HLAE/CS2
capture remotely, without opening a terminal. Local Studio itself is
unaffected by pairing (or skipping it): the local orchestrator and web UI boot
and behave exactly the same either way.

It bundles the same pieces `scripts/local-studio.ps1` runs:

- `zv-orchestrator.exe` - spawned directly (not via `zv serve`), so quitting
  the app kills the real server instead of leaving an orphaned grandchild
  holding the port and the SQLite job db. Runs with `ZV_DATABASE_URL=sqlite`
  (job state persists in `<userData>/data/jobs.db` across restarts) and
  `ZV_DATA_DIR=<userData>/data`; HLAE/CS2/FFmpeg are auto-detected, or use the
  tools provisioned on first boot below.
- The Next.js standalone server - started with Electron's own Node (no separate
  Node runtime shipped), in local mode so the UI proxies the whole pipeline to
  the orchestrator.

Both processes bind loopback (`ZV_HTTP_ADDR=127.0.0.1:<port>`) on ports chosen
once per install and persisted in `<userData>/ports.json`; the web port in
particular must stay stable across launches because the reel library lives in
the browser's `localStorage`, which is keyed by origin (`host:port`).

On first boot the app provisions HLAE, FFmpeg, and yt-dlp (~110 MB total) into
`<userData>/tools`, each verified against a pinned sha256 digest, plus the
music catalog; every download is best-effort, so an offline first boot just
leaves that feature unconfigured until the next launch. The window lands on
`/matches` (the app shell/dashboard, not a single flow), since Studio has both
the demo-upload path and the Twitch stream-clips path.

Capture still needs CS2 + HLAE installed on the machine (Windows + GPU); the app
only removes the setup friction, not that requirement. Job data (demos,
artifacts) is written under the per-user app data dir, not Program Files.

## Cloud pairing

`zv-agent.exe` (`cmd/zv-agent`) persists its pairing config at
`os.UserConfigDir()/fragforge/agent.json`, which on Windows is
`%APPDATA%\fragforge\agent.json` - a machine-wide path shared with any other
FragForge tool that uses the same agent, **not** Electron's per-app `userData`
directory. Studio reads/writes that exact path; it never reimplements or
relocates the agent's own config.

Boot flow, right after the local orchestrator's healthz passes and before the
window loads `/matches`:

- If `agent.json` already exists, Studio spawns `zv-agent.exe` with
  `FRAGFORGE_ORCHESTRATOR_URL=http://127.0.0.1:<local orchestrator port>` (so
  the agent fronts this already-running orchestrator instead of spawning its
  own child), plus `FRAGFORGE_CLOUD_URL` and `FRAGFORGE_WEB_ORIGIN` set to the
  production cloud URL. The agent is best-effort: it is never part of the
  fatal boot sequence. If it exits unexpectedly, Studio logs it and retries
  once after 30s; a second exit within 5 minutes of that retry just logs
  (no crash-loop hammering the control plane).
- If `agent.json` does **not** exist and `<userData>/cloud.json` does not
  contain `{"skipPairing":true}`, Studio shows `pair.html` instead of loading
  `/matches`. That page explains pairing ("genera un código en
  `<cloud URL>/connect` y escríbelo aquí"), takes an 8-character code, and
  offers "Seguir en modo local" to skip. Submitting a code runs
  `zv-agent.exe --pair <code>` (`FRAGFORGE_CLOUD_URL` env, 30s timeout,
  synchronous); on success Studio starts the agent as above and loads
  `/matches`, on failure it re-renders the pairing page with the error.
  Skipping writes `<userData>/cloud.json` (`{"skipPairing":true}`) and loads
  `/matches`; **deleting `cloud.json` re-offers pairing on the next boot.**

`pair.html` is a sandboxed renderer with no preload/IPC, matching every other
screen in this app, so its buttons are plain navigations intercepted by
`main.js`'s `will-navigate` handler (the same trick `loading.html`'s error
screen uses for its retry link):

- The code form is a plain `<form method="GET" action="https://pair.fragforge.invalid/">`;
  submitting it navigates to `https://pair.fragforge.invalid/?code=XXXXXXXX`,
  which `will-navigate` matches by **origin** (the code is a query param) and
  turns into a `zv-agent.exe --pair` call.
- "Seguir en modo local" is `<a href="https://pair-skip.fragforge.invalid/">`.
- The pairing screen is also reachable outside first boot, e.g. from a link
  the hosted web's `/connect` page can show ("open the agent on your PC"):
  `<a href="https://pair-open.fragforge.invalid/">` re-shows `pair.html` at
  any point in the session.

  None of these three hosts ever resolve; Chromium never gets far enough to
  try, because `will-navigate` calls `event.preventDefault()` before handling
  each one. Re-rendering the pairing page after a failed attempt uses
  `executeJavaScript` to fill `pair.html`'s `#status` element (mirroring how
  `loading.html`'s `#status` is updated during boot), since a fresh
  `loadFile()` call is the only way to reload a `file://` page and would lose
  whatever the user had typed.

`FRAGFORGE_CLOUD_URL` and `FRAGFORGE_WEB_ORIGIN` both default to
`https://fragforge.167-233-55-246.sslip.io` and are overridable via the
corresponding environment variables (e.g. for pointing a dev build at a local
cloud stack).

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
stages `zv.exe`, `zv-orchestrator.exe`, `zv-editor.exe`, `zv-agent.exe`, and
the standalone server into `build-resources/`), then `electron-builder`
produces the installer under `dist-installer/` (`FragForge Studio Setup
<version>.exe`, where `<version>` is the `version` field in
`desktop/package.json`). The app icon lives at `build/icon.ico`, which
electron-builder picks up automatically; `assemble.mjs` fails fast if it's
missing. `zv-agent.exe` is required at assemble time (same as `zv.exe`,
`zv-orchestrator.exe`, and `zv-editor.exe`) since pairing depends on it being
bundled; `zv-recorder.exe` is the only optional one, bundled only when it's
present in `bin/`.

This v1 is unsigned, so Windows SmartScreen shows an "unknown publisher" prompt
on first run - choose "More info" -> "Run anyway". Code signing and auto-update
are intentionally out of scope for v1.

## Run without packaging (dev)

```powershell
# From the repo root, once: build the Go binaries and the standalone bundle.
.\scripts\build.ps1
cd desktop; npm install
npm run assemble        # builds the web + stages build-resources/

# In dev, main.js resolves every bundled resource (zv-orchestrator.exe,
# zv-editor.exe, the web server) from .\build-resources, the same layout
# `npm run assemble` stages for packaging. Launch the Electron shell:
npm start
```

## How it works

`main.js` (Electron main process):

1. Reads or picks two per-install-stable loopback ports (`orchestrator`,
   `web`), persisted in `<userData>/ports.json`.
2. Kicks off music catalog provisioning in the background, and awaits
   provisioning of HLAE/FFmpeg/yt-dlp into `<userData>/tools` (first boot
   only; later boots return the cached installs instantly).
3. Spawns `zv-orchestrator.exe` directly - not `zv.exe serve` - so quitting the
   app reliably kills the real server (`ZV_DATABASE_URL=sqlite`,
   `ZV_DATA_DIR=<userData>/data`, `ZV_HTTP_ADDR=127.0.0.1:<orchPort>`, plus any
   provisioned tool paths).
4. Spawns the Next standalone `server.js` via `ELECTRON_RUN_AS_NODE`
   (`NEXT_PUBLIC_FRAGFORGE_MODE=local`, `ORCHESTRATOR_URL` pointing at the
   orchestrator, `PORT=<webPort>`).
5. Waits for `/healthz` and the web root.
6. If already paired (`%APPDATA%\fragforge\agent.json` exists), spawns
   `zv-agent.exe` fronting the local orchestrator; otherwise, unless pairing
   was skipped before, shows `pair.html` instead of the app shell. See "Cloud
   pairing" above for the full flow.
7. Loads `/matches` in the window (directly, or after pairing/skip).
8. Kills the orchestrator, web, and agent children on quit. A single-instance
   lock prevents a second launch from spawning a duplicate backend.
