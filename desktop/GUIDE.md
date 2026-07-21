# FragForge Studio Desktop Guide

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
the browser's `localStorage`, which is keyed by origin (`host:port`). Electron
also rotates a random `discovery_secret` in that file on every boot; it is used
only to authenticate the matching local orchestrator and integrated agent.

The installer bundles the official HLAE archive pinned by `src/hlae-tool.json`.
On first boot the app installs it alongside FFmpeg and yt-dlp into `<userData>/tools`, verifies every pinned SHA-256 digest, and provisions the music catalog.
HLAE is available offline; the remaining downloads are best-effort and retry on the next launch.
After the current HLAE package is verified, Studio removes older versioned HLAE caches.
The packaged HLAE version is intentionally fixed by the manifest so every desktop build is reproducible.
The window lands on `/matches` because Studio has both the demo-upload path and the Twitch stream-clips path.

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

## FragForge Agent

Studio includes a global FragForge Agent rail backed only by the locally
installed `codex app-server`. The connection card opens the official ChatGPT
OAuth flow for the user's personal Codex account. Codex owns and refreshes the
session; FragForge never asks for, stores, or bills an OpenAI API key.

After connecting, the agent can inspect and use Studio's typed operations for
demos, stream clips, renders, captions, killfeed review, QA, publishing assets,
and cleanup. Reads execute directly. Writes, costly work, and destructive work
become exact approval cards, while capture and render also require an approved
creative brief. It can open Studio's native picker to start a local demo or
stream-video import, continue automatically after every approved operation, and
watch queued parsing, capture, analysis, and render states so the next agent
turn starts without another user message. A stream brief snapshots the canonical
saved edit plan and is invalidated, together with prepared render cards, whenever
that plan changes. The approved plan timestamp is also sent as an atomic backend
precondition, so a last-moment edit cannot race past render admission. Public Twitch clip/VOD URLs can also
be supplied in chat. Local demo, video, and voice files stay behind Studio's
file pickers so their paths and raw media never enter model context.

The embedded agent uses Codex app-server dynamic tools directly through the
narrow Studio operation gateway. No external assistant transport or launcher
is shipped.

## xAI subtitle credentials

Every installer remains credential-free. In the installed app, each
Windows user opens `/settings` and enters their own xAI key for stream
subtitles. The page sends the entered value through the narrow Electron preload
bridge directly to the main process; it never goes through the bundled Next.js
server or browser `localStorage`. After saving, the stored key is never returned
to the page or displayed again. The UI exposes only whether a key is configured
and which configuration source is active.

Electron encrypts the saved key with `safeStorage`, backed by Windows DPAPI, so
the encrypted value is tied to the current Windows user. Saving or deleting the
key does not hot-reload the orchestrator: the user must explicitly restart
Studio for the change to take effect. A restart terminates the local child
processes and may interrupt an active upload, capture, or render, so finish
current tasks before applying it.

The runtime precedence is:

1. `XAI_API_KEY` inherited by the desktop process.
2. The current Windows user's encrypted key saved from `/settings`.
3. No xAI credential (automatic transcription is unavailable, but reviewed
   Spanish `caption_words` imported with `zv stream captions` still render).

There is no shared-key or team build mode. Packaging strips `XAI_API_KEY` from
the build, web, and electron-builder environments, and the installer manifest
contains no credential resource. At runtime the selected environment or
per-user credential is supplied only to `zv-orchestrator.exe` for transcription
and removed from the environments of the bundled Next.js server and media
subprocesses.

## Build the installer (on Windows)

Prerequisites: Go 1.26+, Node.js + pnpm, and the web deps installed.

```powershell
# 1. From the repo root: build the Go binaries.
.\scripts\build.ps1

# 2. Install web deps (the assemble step runs the Next.js production build).
cd web; pnpm install; cd ..

# 3. Build the desktop app.
cd desktop
pnpm install
pnpm run dist
```

`pnpm run dist` runs `scripts/assemble.mjs` (builds the web in local mode and
stages `zv-orchestrator.exe`, `zv-editor.exe`, `zv-recorder.exe`, and the
standalone server into `build-resources/`), then `electron-builder` produces the
installer under `dist-installer/` (`FragForge Studio Setup <version>.exe`,
where `<version>` is the `version` field in `desktop/package.json`). The
distribution command verifies the packaged HLAE archive, installer, blockmap,
and checksums before returning success.
The app icon lives at `build/icon.ico`, which electron-builder picks up
automatically;
`assemble.mjs` fails fast if it's missing. `zv-orchestrator.exe`,
`zv-editor.exe`, and `zv-recorder.exe` are required at assemble time so the
packaged app can parse, capture, and render reels. The developer `zv.exe` CLI
stays available in the repository build but is not shipped in the desktop
installer.

The build has one distribution target, `pnpm run dist`. It rejects unsupported
arguments, removes `XAI_API_KEY` from every child build environment, and cannot
stage or declare a credential resource. Users configure credentials after
installation through `/settings`, where Windows DPAPI protects them per user.

The distribution command also creates dist-installer/SHA256SUMS.txt for the
installer and its blockmap, then verifies both before returning success. CI
repeats that verification in a fresh Node process. Publish SHA256SUMS.txt beside
the versioned installer assets in GitHub Releases so recipients can verify the
download before running it.

This v2 is unsigned, so Windows SmartScreen shows an "unknown publisher" prompt
on first run - choose "More info" -> "Run anyway". Code signing and auto-update
are intentionally out of scope for v2.

## Run without packaging (dev)

```powershell
# From the repo root, once: build the Go binaries and the standalone bundle.
.\scripts\build.ps1
cd desktop; pnpm install
pnpm run assemble        # builds the web + stages build-resources/

# In dev, src/main.ts resolves every bundled resource (zv-orchestrator.exe,
# zv-editor.exe, zv-recorder.exe, the web server) from .\build-resources, the
# same layout `pnpm run assemble` stages for packaging. Launch the Electron shell:
pnpm start
```

## How it works

`src/main.ts` (Electron main process, compiled to `dist/main.js`):

1. Reads or picks two per-install-stable loopback ports (`orchestrator`,
   `web`), rotates a 32-byte discovery secret, and persists all three atomically
   in `<userData>/ports.json`.
2. Kicks off music catalog provisioning in the background, and awaits
   provisioning of bundled HLAE plus FFmpeg/yt-dlp into `<userData>/tools`
   (first boot only; later boots return the cached installs instantly).
3. Spawns `zv-orchestrator.exe` directly - without a `zv.exe serve`
   intermediary - so quitting the app reliably kills the real server (`ZV_DATABASE_URL=sqlite`,
   `ZV_DATA_DIR=<userData>/data`, `ZV_HTTP_ADDR=127.0.0.1:<orchPort>`, the
   ephemeral `ZV_DISCOVERY_SECRET`, plus any provisioned tool paths).
4. Spawns the Next standalone `server.js` via `ELECTRON_RUN_AS_NODE`
   (`ORCHESTRATOR_URL` pointing at the orchestrator, `PORT=<webPort>`).
5. Waits for `/healthz` and the web root.
6. Loads `/matches` in the window.
7. Kills the orchestrator and web children on quit. A single-instance lock
   prevents a second launch from spawning a duplicate backend.
