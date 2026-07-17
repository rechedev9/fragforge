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
the browser's `localStorage`, which is keyed by origin (`host:port`). Electron
also rotates a random `discovery_secret` in that file on every boot; it is used
only to authenticate the matching local orchestrator to the MCP.

The installer bundles the official HLAE 2.191.0 release. On first boot the app
installs it alongside FFmpeg and yt-dlp into `<userData>/tools`, each verified
against a pinned sha256 digest, plus the music catalog. HLAE is available
offline; the remaining downloads are best-effort and retry on the next launch.
After the current HLAE package is verified, Studio removes older versioned HLAE
caches. The HLAE version is intentionally fixed instead of following the latest
release so every desktop build uses the same official package. The window
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

## Model Context Protocol (MCP)

This source tree embeds a dependency-free TypeScript MCP server over stdio.
Codex and Claude Code can operate the same local pipeline as the UI without a
hosted backend, browser automation, raw shell commands, or a second API.

The MCP follows Cloudflare's progressive-disclosure idea without evaluating
model-generated JavaScript:

- `search` ranks the allowlisted operation catalog and returns the exact JSON
  input schema. With partial arguments it also resolves live values: job IDs,
  roster players/SteamIDs, moments/segment IDs, presets, music, render artifact
  names, and capture/render readiness.
- `execute` validates an exact operation and arguments. Reads run immediately.
  Writes, captures, renders, and deletes return a side-effect-free preview by
  default; they run only with `mode: "apply"` and `confirmed: true`.
- `fragforge://catalog` and `fragforge://status` expose the static catalog and
  live readiness as MCP resources. Clients advertising MCP elicitation can be
  asked for a missing scalar input during a tool call.

Operations cover Studio status and metrics, catalogs, CS2 demo upload/scan/parse,
record/generate/compose, render state/QA/publishing/artifacts, and stream/VOD
jobs, exact edit plans, source/video URLs, and subtitle configuration. Binary
media never enters model context; artifact operations return loopback URLs when
Studio uses its normal loopback read mode.

MCP cancellation follows the protocol's fire-and-forget semantics: cancelled
requests receive no response. If `streams.create_from_file` is cancelled after
the orchestrator accepted its upload, search and execute `streams.list`, then
use `streams.get` with the returned ID before considering a retry. This recovers
the durable job without uploading the same video twice.

### Repository development setup

The repository already contains both client configurations:

- Codex: `.codex/config.toml` (registered but disabled by default)
- Claude Code: `.mcp.json`

Codex Desktop uses `.\bin\zv.exe` as its primary FragForge interface and does
not need Studio for normal CLI work. Start each task with
`.\bin\zv.exe capabilities --format json`, then inspect
`.\bin\zv.exe flows show demo --format json` or `flows show stream` before
crossing the player/clip selection and expensive capture/render boundaries.
Both journeys expose vertical 9:16 and landscape 16:9 delivery through the
same structured CLI. To opt into MCP, set `enabled = true` in
the `mcp_servers.fragforge` block, start FragForge Studio, and then open a new
Codex session. The MCP launchers use Node's built-in TypeScript stripper, so
use Node 22.10+.

Launch the client from the repository root (for example,
`codex --cd C:\Users\reche\Documents\zackvideo`), not from `desktop/`: the
checked-in `cwd = "."` and TypeScript entry paths are intentionally root-relative.
From `desktop/`, the same server can be run directly with `pnpm run mcp`.
Claude Code shows a newly discovered project MCP as pending until the user
approves it once; this is expected trust behavior for `.mcp.json`.

The MCP discovers the desktop's stable orchestrator port from the same
`<userData>/ports.json` used by Electron. Development and diagnostics can
override this without editing config:

```powershell
$env:FRAGFORGE_ORCHESTRATOR_URL = "http://127.0.0.1:8080"
$env:FRAGFORGE_MCP_TIMEOUT_MS = "30000"
pnpm run mcp
```

For a token-protected orchestrator, pass the token only through
`FRAGFORGE_MUTATION_TOKEN`; it becomes `X-FragForge-Token` internally and is
never returned or logged. `FRAGFORGE_PORTS_FILE` overrides port discovery. A
desktop port and its fresh `discovery_secret` are read from one snapshot; the
server must prove possession through a bounded HMAC challenge before the MCP
sends a token or local media. The proof is available only to loopback peers,
and the discovery secret is never sent as an API header or returned/logged.
Older manually maintained port files may omit it only when
`FRAGFORGE_MUTATION_TOKEN` supplies the HMAC fallback. Tokenless automatic
discovery without either secret is rejected. An explicit
`FRAGFORGE_ORCHESTRATOR_URL` remains an intentional trust override. Only HTTP
loopback URLs are accepted, redirects are rejected, and stdout is reserved
exclusively for MCP JSON-RPC.

The normal desktop bind does not authenticate reads, so artifact URLs open in a
browser. If a custom orchestrator enables token authentication for read routes,
API operations still work but `artifacts.get_url` and
`artifacts.get_stream_url` return an explicit unsupported-mode error instead of
an unusable bare URL. Use the loopback-only Studio configuration for MCP media
links. Upload requests have a ten-minute server timeout; the checked-in Codex
configuration gives tools fifteen minutes so a valid upload is not cut short.

### Installed Studio setup

Installers built from this source include `fragforge-mcp.cmd` beside the desktop
executable. This source change does not modify any already-published v2.0.3
release asset. The launcher runs the compiled TypeScript server with Electron's
embedded Node runtime, so the end user does not install Node. Keep the normal
Studio window running so its orchestrator is available. For Codex, add this
block to `%USERPROFILE%\.codex\config.toml` (adjust the installation path if
needed):

```toml
[mcp_servers.fragforge]
enabled = true
command = "cmd.exe"
args = ["/d", "/s", "/c", 'C:\Users\<you>\AppData\Local\Programs\FragForge Studio\fragforge-mcp.cmd']
startup_timeout_sec = 10
tool_timeout_sec = 900
default_tools_approval_mode = "writes"
```

The 900-second tool timeout accommodates local uploads and capture preparation;
the `writes` approval mode keeps applying mutations behind client approval.
Claude Code can register the same launcher once with:

```powershell
claude mcp add --transport stdio --scope user fragforge -- cmd.exe /d /s /c "C:\Users\<you>\AppData\Local\Programs\FragForge Studio\fragforge-mcp.cmd"
```

Restart/open a new client session after registration. A typical agent flow is:

1. Search `studio status`, then execute the read-only `studio.status`.
2. Search `create demo job` and execute the upload preview.
3. Apply the approved upload; search `jobs.parse` with its `job_id` to discover
   roster inputs, or poll `jobs.get` if a SteamID was supplied.
4. Search `jobs.generate` with the job ID to discover segments, presets, and
   music; preview first, then apply only after approval.
5. Poll `renders.get`, read QA/publish metadata, and request an artifact URL.

The launcher uses `ELECTRON_RUN_AS_NODE`, so the MCP process never loads the
Electron main process, acquires its single-instance lock, or opens a window. It
also never starts HLAE/CS2 merely because the server connects; only a confirmed
costly operation can enqueue capture or render work.

### MCP evaluation feedback loop

The standalone evaluator uses an isolated in-memory orchestrator, fresh
temporary data, authenticated `ports.json` discovery, and inaccessible
sentinel paths for every external capture/render tool. It starts the real Go
and MCP stdio processes, scores protocol, discovery, safety, validation,
upload, artifact, HMAC, and shutdown scenarios, and exits non-zero on any
failure:

```powershell
cd desktop
pnpm run eval:mcp:gate
```

Every run writes timestamped JSON and Markdown plus `latest.json` and
`latest.md` under `data/mcp-evals/`. Use the Markdown feedback queue to fix the
root cause, rerun the gate, and require 100/100 on fresh runs. The gate always
rebuilds `bin/zv-orchestrator.exe`, so a stale local binary cannot make a source
change appear green. This complements, rather than replaces,
`pnpm run test:mcp:e2e` and the packaged-launcher test.

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
where `<version>` is the `version` field in `desktop/package.json`). The app
then runs a mandatory stdio handshake against the real
`dist-installer/win-unpacked/fragforge-mcp.cmd` and its packaged `app.asar`;
the distribution command fails if either artifact is absent or unusable. Run
that gate separately after packaging with `pnpm run test:mcp:packaged`. Run all
MCP E2E tests with a mandatory real Go orchestrator using `pnpm run test:mcp:e2e`.
Run the scored real-process feedback gate with `pnpm run eval:mcp:gate`.
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
8. The separately packaged `fragforge-mcp.cmd` launcher uses Electron's Node
   mode to run the stdio server against the already-running orchestrator.
