# FragForge

Deterministic CS2 demo-to-video pipeline. Drop a `.dem`, describe the clip you
want, and get an upload-ready vertical Short: always 1080x1920 at 60fps, with
viral-style editing by default.

FragForge ships as a Windows desktop installer.
Download it from the [`landing/`](./landing) site - there is no hosted service to sign up for.

It parses demos into kill plans, records gameplay with HLAE/CS2, and
post-processes clips with FFmpeg, Lua effects, overlays, and publishing
metadata. Capture and rendering run locally on Windows; optional stream-caption
audio is sent to xAI only when cloud subtitles are enabled.

```text
.dem + prompt
  -> parse demo into a kill plan and scored moments
  -> record the right segments with HLAE/CS2
  -> render the Short with a viral preset (1080x1920 @ 60fps)
  -> publish pack: MP4, cover, caption, gallery, manifest
```

## The one command

```powershell
zv short match.dem --prompt "las mejores kills de martinez" --target-steamid 76561198148986856
```

`zv short` chains parse -> moments -> HLAE/CS2 recording -> render. The prompt
is interpreted deterministically (Spanish and English):

- A 17-digit number or `--target-steamid` selects the target player.
- `mejores` / `best` / `highlights` selects the top moments; otherwise all kills
  are compiled into one Short.
- `música` / `music` / `beat` adds beat analysis for the selected/default
  preset (requires `--music <audio>`).
- An explicit preset name in the prompt (or `--preset`) overrides the default.
- Anything else falls back to the default preset, `viral-60-clean`.

Useful flags:

| Flag | Purpose |
|------|---------|
| `--dry-run` | Print the resolved plan (player, selection, preset, output spec) without running anything. |
| `--from-recording <recording-result.json>` | Skip parse + record and render from an existing recording. |
| `--music <audio>` | Music track for beat-synced montages. |
| `--hlae`, `--cs2` | Tool paths (or `ZV_HLAE_PATH` / `ZV_CS2_PATH`). |
| `--out <dir>` | Output directory. |

The command prints a plan summary before running and stage-by-stage progress
(`[1/4] parse`, ...). If a stage fails, rerun with `--from-recording` instead of
recording again.

## Web UI (Local Studio)

Prefer the browser? Local Studio runs the whole product from the web UI on your
own Windows + GPU PC, capture included:

```powershell
.\scripts\local-studio.ps1
```

It starts the orchestrator with a persistent local SQLite job database and an
in-process queue (HLAE/CS2 auto-detected), then starts the web UI and opens
`http://localhost:3000/upload`.
The flow is: upload a demo -> pick a player -> pick specific kills -> create the
reel, at which point HLAE + CS2 open to capture and the edit is applied.

### Codex and Claude Code (MCP)

This source tree includes a local TypeScript MCP server for FragForge Studio,
so Codex or Claude Code can inspect live jobs, discover
players/segments/presets/songs, start
approved pipeline actions, follow render status, and obtain artifact URLs.
It uses a Cloudflare-inspired progressive-disclosure contract: only `search`
and `execute` are registered, while each operation's exact input schema and
current allowed values are returned on demand. Mutations are previews unless
the caller explicitly uses `mode=apply` and `confirmed=true`.

For shell-first agents, `zv workflows list --format json` and
`zv workflows show <name> --format json` expose the canonical execution command,
accepted required/value/boolean flags, and safety hints for read-only, dry-run,
and long-running behavior. `zv workflows run <name> -- ...` validates that same
contract before delegating to the stage binary.

Project-local Codex and Claude Code configuration is already checked in under
`.codex/config.toml` and `.mcp.json`. Start Studio first, then open a new agent
session from this trusted repository's root (not from `desktop/`), because the
checked-in launcher paths are root-relative. Already-published v2.0.3 release
assets are unchanged; an installer must be rebuilt and published separately to
include this MCP launcher. See the complete setup, installed-app
commands, operation model, and security notes in
[`desktop/README.md`](./desktop/README.md#model-context-protocol-mcp).
For deterministic source evaluation, run `cd desktop; pnpm run eval:mcp:gate`;
it rebuilds the local orchestrator and writes scored JSON/Markdown reports to
`data/mcp-evals/`.

### Manual YouTube publication assistant

Every finished reel in the Library has a **PREPARAR PUBLICACIÓN** action. It
builds a seven-day schedule in `Europe/Madrid`, three to five factual metadata
alternatives, keywords, and tags from the reel itself. The title and description
remain editable, and the dialog can copy each field, download the MP4, and open
the stable [YouTube Studio](https://studio.youtube.com/) home page in the system
browser.

FragForge does not choose the channel, audience, visibility, or publication
date. In YouTube Studio, follow the official **CREAR -> Subir vídeos** flow and
complete those decisions there; see [YouTube's official upload guide](https://support.google.com/youtube/answer/57407?hl=es).
No Google project or account connection is required by FragForge.

The assistant uses this deterministic local-time Shorts reference:

| Day | Recommended local hours, in order |
|-----|-----------------------------------|
| Monday | 20:00, 17:00, 18:00 |
| Tuesday | 20:00, 21:00, 19:00 |
| Wednesday | 19:00, 20:00, 21:00 |
| Thursday | 19:00, 20:00, 21:00 |
| Friday | 16:00, 18:00, 19:00 |
| Saturday | 19:00, 11:00, 18:00 |
| Sunday | 19:00, 20:00, 17:00 |

If `FIRECRAWL_API_KEY` is present server-side, Studio also performs a bounded
monthly search for recent public CS2 Shorts and shows the extracted terms as
trend hints. Firecrawl results never masquerade as YouTube views or ranking
metrics, and the key is never sent to the renderer. Without the key, the same
schedule and reel-derived recommendations remain available. Suggestions only
use terms that match the player, map, weapon, hook, or kill count from that
request.

Experimental local builds may have left generic Windows Credential Manager
entries named `FragForge/YouTube/OAuthClient`,
`FragForge/YouTube/Connection`, or `FragForge/YouTube/Upload/<id>`. Current
builds do not read or delete them. Remove them manually from **Credential
Manager -> Windows Credentials** if desired.

## Render preset

The single supported preset lives in `internal/editor/preset.go`: `viral-60-clean`.
The loadout catalog (`internal/renderplan`), the
HTTP API (`/api/presets`, `/api/loadouts`, render-variant validation), the
workbench UI, and the render worker all derive from that registry. The preset
outputs 1080x1920 at 60fps. Unknown preset names are rejected with the valid
list.

List them any time with `zv presets` (`--format json` for automation).

| Preset | What it does |
|--------|--------------|
| `viral-60-clean` (default) | Clean HUD-less POV with kill notices, viral hook text, punch-ins, kill counter, and milestone labels. |

The editing choices behind the viral presets: hook text in the first 1-2s,
punch-ins on kills, slow-mo only on the final kill, beat-synced drops,
loop-friendly endings, never cropping the killfeed.

## Quick start

Requires Go 1.26+, FFmpeg, and (for recording) CS2 plus HLAE.

```powershell
# Build all binaries into .\bin
.\scripts\build.ps1

# See what a run would do
.\bin\zv short testdata\match.dem --prompt "all kills" --target-steamid 76561198000000000 --dry-run

# Sanity-check the project contract (skills, workflows, docs)
.\bin\zv check
```

Unix-like shells can use `make build` / `make test` instead.

### Orchestrator (HTTP API + workers)

```bash
export ZV_DATABASE_URL=memory   # in-memory job repo + inline queue, no external services needed
export ZV_DATA_DIR="./data"

./bin/zv serve
```

The server binds to `127.0.0.1:8080` by default; binding to a non-loopback
address requires `ZV_MUTATION_TOKEN`. Optional environment variables:

| Variable | Purpose |
|----------|---------|
| `ZV_HTTP_ADDR` | Listen address (default `127.0.0.1:8080`). |
| `ZV_HLAE_PATH`, `ZV_CS2_PATH` | Recording tool paths. |
| `ZV_RECORDER_PATH`, `ZV_COMPOSER_PATH`, `ZV_FFMPEG_PATH` | Stage binary overrides. |
| `ZV_WORKER_CONCURRENCY` | Asynq worker concurrency (default 2). |
| `ZV_MEDIA_WORK_DIR` | Keep media workdirs for debugging (deleted after each task when unset). |
| `ZV_CODEX_PATH`, `ZV_CODEX_MODEL`, `ZV_AGENT_TIMEOUT` | Optional local editorial assistant (`codex exec`, read-only sandbox) for caption/title suggestions. |
| `XAI_API_KEY` | Optional xAI speech-to-text plus Grok Spanish translation for stream captions. |
| `FIRECRAWL_API_KEY` | Optional public CS2 Shorts trend hints for the publication assistant; never sent to the browser. |

xAI captions use the REST `/v1/stt` endpoint, which returns word-level timestamps
and accepts the speech-oriented WAV extracted from the selected source range.
The endpoint does not take a model name, and its `language` field only formats
numbers, currencies, and units; it does not select or translate the spoken
language. FragForge therefore transcribes with automatic source-language
recognition, then uses `grok-4.5` structured output to preserve Spanish speech
or translate every other recognized phrase into Spanish before burning captions.
xAI prices batch speech-to-text separately from streaming; check the
[current Voice API pricing](https://x.ai/api/voice).
Stream captions use xAI only. A normal captioned clip makes one transcription
request and one Spanish preservation/translation request. If the whole-range
result is unusable or is valid but temporally sparse, FragForge makes one
speech-enhanced locator request and then re-transcribes at most four short,
padded regions from the original audio that collectively cover the selected
range; locator timings choose useful boundaries, but locator text is never
published. A failed recovery does not discard an otherwise valid first pass. If
no pass produces usable words or timings, the clip is published uncaptioned with
a warning rather than burning a hallucinated transcript or substituting another
engine. A transport, authentication, or Spanish-translation failure remains a
hard render error because publishing non-Spanish text would violate the selected
subtitle contract.

Source builds and every desktop installer do not contain a credential.
In the installed desktop app, each Windows user can open `/settings` and save
their own xAI key. Electron encrypts it for that Windows user with
`safeStorage` (backed by DPAPI); the stored value is never returned for display,
sent to the bundled Next.js server, or written to browser `localStorage`. Saving
or deleting it takes effect only after an explicit Studio restart. Restarting
stops the local orchestrator and can interrupt active uploads, captures, or
renders, so finish those tasks first.

xAI credential precedence is: an inherited `XAI_API_KEY`, then the encrypted
per-user key from `/settings`, then no xAI credential. Every desktop build is
credential-free: the packaging scripts remove `XAI_API_KEY` from child build
environments and expose no target or resource capable of embedding a shared
key. See `desktop/README.md` for the desktop and build details.

Set a newly generated key in the same PowerShell session that starts Local
Studio without putting the secret in command history:

```powershell
$secureKey = Read-Host "xAI API key" -AsSecureString
$env:XAI_API_KEY = (New-Object System.Net.NetworkCredential("", $secureKey)).Password
.\scripts\local-studio.ps1
```

Validate a real clip against xAI without printing the key or transcript:

```powershell
.\scripts\smoke-xai-stt.ps1 -MediaPath .\data\clip.mp4 -Language es -ASSPath .\data\clip.ass -ExpectedText "texto conocido de la fixture"
```

Validate the complete automatic-STT plus Grok 4.5 Spanish output path:

```powershell
.\scripts\smoke-xai-spanish.ps1 -MediaPath .\data\smoke\xai-stt\voice-es.wav -ASSPath .\data\smoke\xai-stt\voice-es-preserved.ass -ExpectedSpanish "FragForge crea subtítulos precisos para cada clip del directo"
.\scripts\smoke-xai-spanish.ps1 -MediaPath .\data\smoke\xai-stt\voice-en.wav -ASSPath .\data\smoke\xai-stt\voice-en-spanish.ass
```

### Smoke tests

```bash
# Parser-only (requires a .dem in testdata/)
./scripts/smoke.sh testdata/<your-demo>.dem <SteamID64>
```

```powershell
# Full real run against a running orchestrator with recorder/composer configured
.\scripts\smoke-real.ps1 -Demo testdata\lavked-vs-tnc-m2-nuke.dem -TargetSteamID 76561198148986856
```

The real smoke uploads the demo, waits for `parsed`, records, retries `record`
once to verify artifact skipping, composes, retries `compose`, then downloads
the final MP4 and validates H.264, 1920x1080, 60fps when `ffprobe` is
available.

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/jobs` | Multipart upload: `demo` file + `config` JSON (`{"target_steamid":"..."}`). Returns `201 {id, status}`. |
| GET | `/api/jobs/{id}` | Job metadata and status; `?view=status` returns the lightweight polling representation. |
| GET | `/api/jobs/{id}/plan` | Kill plan JSON (200) or 409 if not ready. |
| POST | `/api/jobs/{id}/record` | Enqueue recording after parse approval. |
| POST | `/api/jobs/{id}/compose` | Enqueue final composition after recording. |
| GET | `/api/jobs/{id}/final` | Stream the composed MP4 when ready. |
| GET | `/api/presets` | Render preset registry as JSON (name, geometry, behavior flags, default). |
| GET | `/api/stream-variants` | Stream/VOD render variant registry, including the default; MCP input discovery derives live choices from this endpoint. |
| GET | `/api/jobs/{id}/renders/{variant}/videos/{name}/publish-assistant?days=7` | Reel metadata, factual suggestions, Madrid schedule, optional trend hints, and the stable YouTube Studio URL. |

`POST /record` is accepted for `parsed` and `recorded` jobs; `POST /compose`
for `recorded` and `composed` jobs. Manual retries are idempotent: workers skip
external media commands when the durable artifacts already exist. Render
variant requests are validated against the preset registry; scored moments
default to `viral-60-clean`.

## CLI reference

`zv` is the unified entrypoint. Stage commands remain available for granular or
scripted use:

```bash
./bin/zv demo parse --demo match.dem --steamid 76561198000000000 --out plan.json
./bin/zv demo players --demo match.dem
./bin/zv record --killplan plan.json --demo match.dem --out run/recording --hlae <HLAE.exe> --cs2 <cs2.exe>
./bin/zv shorts render --recording-result run/recording/recording-result.json --out run/shorts --preset viral-60-clean
./bin/zv compose final --recording-result run/recording/recording-result.json --out run/final.mp4
./bin/zv music analyze --input track.mp3 --killplan plan.json --out run/rhythm.json
./bin/zv presets
./bin/zv check
./bin/zv serve
```

Other command groups: `zv utility audit` (lineup catalogs), `zv analysis`
(tactical data and viewers), `zv gallery open`, `zv skills` and `zv workflows`
(repo-local agent skills and the cataloged workflow contract; both support
`--format json`). Legacy binaries stay reachable through pass-throughs such as
`zv parser`, `zv editor`, `zv recorder`, `zv composer`, and `zv orchestrator`.

`zv shorts render` options worth knowing:

- `--segments seg-001,seg-004` / `--limit N` for fast partial iteration, plus
  `--skip-existing` and `--open-gallery`.
- `--render-jobs N` caps how many shorts render concurrently (default 0 =
  automatic CPU-based limit; pass 1 to force sequential rendering).
- `--dry-run` writes planned manifests, captions, FFmpeg commands, and cover
  prompts without rendering.
- `--music`, `--rhythm`, `--compile-segments` for music-scripted compilation
  edits (analyze the track first with `zv music analyze`).
- `--effects-preset viral-ultra-clean` or `--effects <script.lua>` for explicit
  custom Lua effects. The Lua DSL
  exposes `on_segment`, `on_kill`, `on_smoke`, `zoom`, `flash`, `text`, and
  `grade`; scripts run sandboxed (no filesystem/process access) with a capped
  evaluation budget. Standard kill/highlight renders use `viral-ultra-clean`.
- `--music`, `--rhythm`, and `--compile-segments` for music-synced
  compilations with the same `viral-60-clean` visual standard.

Every render writes a publish pack under `shorts/publish/`: clean MP4
filenames, caption files, cover JPGs, `pack-manifest.json`,
`publish-summary.md`, and an `index.html` review gallery with copy buttons.
Outputs are validated as 1080x1920 H.264 60fps and warned if they exceed the
180s Shorts limit.

## Media artifacts and cleanup

Durable local storage keeps, per job: `recording/recording-result.json`,
`recording/recording.js`, `recording/segments/*.mp4`, `shorts/*` (manifest,
result, prompts, publish pack), and `composition/final.mp4` with its result
JSON. Treat `data/` as output unless you are explicitly working on fixtures.

Local edit experiments can pile up `shorts*` directories. The cleanup script
previews by default and only deletes with `-Apply`:

```powershell
.\scripts\cleanup-artifacts.ps1            # preview targets and estimated space
.\scripts\cleanup-artifacts.ps1 -Apply     # delete regenerable variants, keep baselines
```

Pass `-RunDir` and comma-separated `-KeepShortsDir` values to clean a different
run. Never commit generated video/audio/image artifacts to git.

## Repository layout

- `cmd/` — thin CLI entrypoints (`zv`, `zv-parser`, `zv-recorder`,
  `zv-composer`, `zv-editor`, `zv-orchestrator`, ...).
- `internal/parser` — `.dem` parsing and segment extraction.
- `internal/killplan` — shared kill/segment plan types.
- `internal/moments` — scored, reviewable clip candidates from kill plans.
- `internal/recording` — HLAE/CS2 recording scripts and validation.
- `internal/editor` — Shorts rendering, the preset registry, Lua effects,
  validation, publish packs.
- `internal/renderplan` — render variants, loadouts, edit documents, QA.
- `internal/composition` — concat/composition planning and FFmpeg boundaries.
- `internal/httpapi` — orchestrator HTTP routes, handlers, and the embedded
  workbench UI.
- `internal/workers` — Asynq parser/media/agent workers.
- `internal/youtubeinsights`, `internal/youtubetrends` — explainable Madrid
  scheduling, factual metadata recommendations, and optional bounded Firecrawl
  discovery for the manual publication assistant.
- `internal/storage`, `internal/job`, `internal/tasks` — persistence and job
  state.
- `effects/` — editable Lua effect scripts.

## Tests

```bash
make test            # also runs `zv check` to keep the CLI contract aligned
scripts/go-gate.sh   # main verification gate (fmt, vet, build, tests)
```

Worker integration tests that need a real demo skip automatically when
`TEST_DEMO_PATH` is unavailable. Tests never launch HLAE/CS2 or long FFmpeg
renders.
