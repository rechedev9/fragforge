# zackvideo

Pipeline for generating CS2 highlight clips from `.dem` files. See
[`docs/README.md`](./docs/README.md) for design docs and
[`docs/toolchain.md`](./docs/toolchain.md) for the external tools used by the
local capture/edit workflow.

## Status

- ✅ `zv` unified CLI façade for local workflows and agent skills
- ✅ `zv-parser` CLI: demo → kill plan JSON
- ✅ `zv-recorder` local HLAE/CS2 recording CLI
- ✅ `zv-composer` local concat composer
- ✅ `zv-editor` local 9:16 shorts editor
- ✅ Local Lua effects scripting for `zv-editor`
- ✅ Smoke-lineup segment mode and Lua annotations for utility clips
- ✅ `zv-pipeline` local recorder → composer runner
- ✅ `zv-orchestrator` HTTP API + Asynq parser/media workers
- ✅ Minimal media workdir cleanup policy
- ✅ `services/cs2-market` Python market intelligence prototype for CS2 item signals
- ⏳ Music sync, advanced overlays, frontend

## Quick start (local development)

Requires Go 1.26+. The orchestrator also needs Docker for local Postgres and
Redis. Unix-like shells can use `make`; on Windows, use the PowerShell scripts
under `scripts/`.

```bash
# 1. Bring up Postgres + Redis
make up
# wait ~10s for healthchecks
make migrate-up  # requires ZV_DATABASE_URL exported, see below
# If the DB already had the original parser-only schema:
# make migrate-media-up

# 2. Set env
export ZV_DATABASE_URL="postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable"
export ZV_REDIS_ADDR="localhost:6379"
export ZV_DATA_DIR="./data"
# Optional:
# Defaults to 127.0.0.1:8080. Binding to 0.0.0.0 requires ZV_MUTATION_TOKEN.
# export ZV_HTTP_ADDR="127.0.0.1:8080"
# export ZV_MUTATION_TOKEN="local-random-token-for-non-loopback-bind"
# Optional local editorial assistant. The worker runs `codex exec` in read-only
# sandbox mode and stores JSON caption/title suggestions as render artifacts.
# export ZV_CODEX_PATH="codex"
# export ZV_CODEX_MODEL="gpt-5.4"
# export ZV_AGENT_TIMEOUT="5m"
# export ZV_WORKER_CONCURRENCY="2"
# export ZV_RECORDER_PATH="/path/to/zv-recorder"
# export ZV_HLAE_PATH="/path/to/HLAE.exe"
# export ZV_CS2_PATH="/path/to/cs2.exe"
# export ZV_COMPOSER_PATH="/path/to/zv-composer"
# export ZV_FFMPEG_PATH="/path/to/ffmpeg"
# export ZV_MEDIA_WORK_DIR="./data/work" # set only when you want to keep media workdirs

# 3. Build binaries
make build

# 4. Run the orchestrator
./bin/zv serve
```

On Windows:

```powershell
.\scripts\build.ps1
```

In another terminal:

```bash
# Parser-only smoke-test (requires a .dem in testdata/)
./scripts/smoke.sh testdata/<your-demo>.dem <SteamID64>
```

On Windows, with the real recorder/composer paths configured and
`zv-orchestrator` already running:

```powershell
.\scripts\smoke-real.ps1 `
  -Demo testdata\lavked-vs-tnc-m2-nuke.dem `
  -TargetSteamID 76561198148986856
```

The real smoke uploads the demo, waits for `parsed`, records, retries
`record` once to verify artifact skipping, composes, retries `compose`, then
downloads the final MP4 and prints the job id, phase timings, output path, and
file size. If `ffprobe` is available through `ZV_FFPROBE_PATH` or `PATH`, it
also checks for H.264, 1920x1080, 60fps video and an audio stream.

## API

| Method | Path                       | Description                                |
|--------|----------------------------|--------------------------------------------|
| POST   | `/api/jobs`                | Multipart upload: `demo` file + `config` JSON (`{"target_steamid":"..."}`). Returns `201 {id, status}`. |
| GET    | `/api/jobs/{id}`           | Job metadata and status.                   |
| GET    | `/api/jobs/{id}/plan`      | Kill plan JSON (200) or 409 if not ready.  |
| POST   | `/api/jobs/{id}/record`    | Enqueue recording after parse approval.    |
| POST   | `/api/jobs/{id}/compose`   | Enqueue final composition after recording. |
| GET    | `/api/jobs/{id}/final`     | Stream the composed MP4 when ready.         |
| GET    | `/api/presets`             | Render preset registry as JSON (name, description, geometry, behavior flags, default). |

`POST /record` is accepted for `parsed` and `recorded` jobs. `POST /compose`
is accepted for `recorded` and `composed` jobs. These manual retries are
idempotent: workers skip external media commands when the durable artifacts
already exist.

## Render presets

All render presets live in one registry: `internal/editor/preset.go`. Adding a
preset means adding one entry there; the loadout catalog
(`internal/renderplan`), the HTTP API (`/api/presets`, `/api/loadouts`,
`/renders/{variant}` validation), the workbench UI, and the render worker all
derive from it. Every preset outputs 1080x1920 at 60fps. The product default is
`viral-60`: drop a demo plus a prompt, get a viral-edited vertical Short. See
`docs/research/11-viral-cs2-vertical-editing.md` for the rationale.

- `viral-60` (default): full-UI 60fps gameplay with hook text, punch-ins, and kill counter overlays.
- `viral-beatsync`: viral-60 for montages with cuts on the detected beat grid (needs music + rhythm json).
- `short-clean`: restrained labels, vertical POV crop, subtle kill punch-ins.
- `short-premium-player`: short-clean plus a player cutout overlay and larger headline.
- `viral-square`: blurred vertical background with centered square gameplay.
- `natural-hq`: unmodified gameplay at higher encode quality for clean local masters.
- `natural-hq2`: natural-hq plus FFmpeg quality checks and contact sheets.
- `natural-hq2-full`: continuous full-UI crop with a mild saturation lift, no scripted effects.
- `natural-hq2-full-plus`: experimental full-frame variant with stronger FFmpeg-only color and mastering.
- `natural-hq3` / `natural-hq3-smooth`: experimental high-encode comparison presets.
- `smoke-lineups`: educational overlays and slow motion for utility throws.

Demo+prompt-to-Short workflow at a high level: upload a demo (`POST /api/jobs`),
the parser builds a kill plan and scored moments (default variant `viral-60`),
recording captures segments, then `POST /api/jobs/{id}/renders/{variant}`
renders upload-ready Shorts. Unknown variants are rejected with the valid
preset list.

## Media artifacts and cleanup

Durable local storage keeps:

- `jobs/{id}/recording/recording-result.json`
- `jobs/{id}/recording/recording.js`
- `jobs/{id}/recording/segments/*.mp4`
- `jobs/{id}/shorts/*.mp4` when generated by `zv-editor`
- `jobs/{id}/shorts/edit-manifest.json`
- `jobs/{id}/shorts/shorts-result.json`
- `jobs/{id}/shorts/prompts/*.md`
- `jobs/{id}/shorts/publish/*.mp4`
- `jobs/{id}/shorts/publish/*.caption.txt`
- `jobs/{id}/shorts/publish/*.cover.jpg`
- `jobs/{id}/shorts/publish/pack-manifest.json`
- `jobs/{id}/shorts/publish/publish-summary.md`
- `jobs/{id}/shorts/publish/index.html`
- `jobs/{id}/composition/composition-result.json`
- `jobs/{id}/composition/final.mp4`

If `ZV_MEDIA_WORK_DIR` is unset, media workers use temporary workdirs and delete
them when a task finishes. If `ZV_MEDIA_WORK_DIR` is set, workdirs are preserved
for debugging.

### Cleaning local artifacts

Local edit experiments can create multiple `shorts*` directories under a run.
Use the cleanup script to keep the current realistic baseline and remove
regenerable variants:

```powershell
# Preview only; prints targets and estimated space.
.\scripts\cleanup-artifacts.ps1

# Delete verified `shorts*` variants except the natural HQ baselines.
.\scripts\cleanup-artifacts.ps1 -Apply
```

By default this targets
`data\tries\faceit-288b1d33-martinez\run-004`, preserves `recording/`, root
run files such as `final.mp4`, `shorts-natural-hq-full`, and
`shorts-natural-hq2-full`. Pass `-RunDir` and comma-separated `-KeepShortsDir`
values to clean another local run.

## Standalone CLI

`zv` is the preferred local entrypoint. It keeps the older focused binaries
available, but gives humans, scripts, and Codex skills one stable command tree:

```bash
./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv demo players --demo testdata/foo.dem
./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts-natural-hq2-full --preset natural-hq2-full
./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json
./bin/zv gallery open --path data/runs/run-004/shorts-natural-hq2-full/publish/index.html
./bin/zv check
./bin/zv check --format json
./bin/zv serve
./bin/zv pipeline --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv skills list
./bin/zv skills show zackvideo-cheater-pov-reels
./bin/zv skills show zackvideo-cs2-utility-shorts
./bin/zv skills show zackvideo-lineup-audit
./bin/zv skills show zackvideo-music-scripted-shorts
./bin/zv skills show zackvideo-shorts-production
./bin/zv skills show zackvideo-youtube-shorts-publish
./bin/zv skills check
./bin/zv skills list --format json
./bin/zv skills show zackvideo-cheater-pov-reels --format json
./bin/zv skills show zackvideo-cs2-utility-shorts --format json
./bin/zv skills show zackvideo-lineup-audit --format json
./bin/zv skills show zackvideo-music-scripted-shorts --format json
./bin/zv skills show zackvideo-shorts-production --format json
./bin/zv skills show zackvideo-youtube-shorts-publish --format json
./bin/zv skills check --format json
./bin/zv workflows list
./bin/zv workflows list --format json
./bin/zv workflows show demo-parse
./bin/zv workflows show demo-parse --format json
./bin/zv workflows show demo-players
./bin/zv workflows show demo-players --format json
./bin/zv workflows show utility-audit
./bin/zv workflows show utility-audit --format json
./bin/zv workflows show record
./bin/zv workflows show record --format json
./bin/zv workflows show compose-final
./bin/zv workflows show compose-final --format json
./bin/zv workflows show music-analyze
./bin/zv workflows show music-analyze --format json
./bin/zv workflows show shorts-render
./bin/zv workflows show shorts-render --format json
./bin/zv workflows show analysis-tactical-data
./bin/zv workflows show analysis-tactical-data --format json
./bin/zv workflows show analysis-viewer
./bin/zv workflows show analysis-viewer --format json
./bin/zv workflows show gallery-open
./bin/zv workflows show gallery-open --format json
./bin/zv workflows show serve
./bin/zv workflows show serve --format json
./bin/zv workflows show pipeline
./bin/zv workflows show pipeline --format json
./bin/zv workflows show skills-check
./bin/zv workflows show skills-check --format json
./bin/zv workflows show workflows-check
./bin/zv workflows show workflows-check --format json
./bin/zv workflows show project-check
./bin/zv workflows show project-check --format json
./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json
./bin/zv workflows run demo-players -- --demo testdata/foo.dem
./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv
./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4
./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json
./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts-natural-hq2-full --preset natural-hq2-full
./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000
./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json
./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts-natural-hq2-full/publish/index.html
./bin/zv workflows run serve
./bin/zv workflows run pipeline -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
./bin/zv workflows run skills-check
./bin/zv workflows run skills-check -- --format json
./bin/zv workflows run workflows-check
./bin/zv workflows run workflows-check -- --format json
./bin/zv workflows run project-check
./bin/zv workflows run project-check -- --format json
./bin/zv workflows check
./bin/zv workflows check --format json
```

`zv check` is the one-command project contract for skills, workflow catalog,
and current workflow docs. `zv skills list`, `zv skills show <name>`, `zv skills check`,
`zv workflows list`, and `zv workflows show <name>` accept `--format json`
when a script or skill needs machine-readable output. `zv workflows run <name>`
executes a cataloged workflow through the same canonical command tree. Workflow
JSON includes both `command` and `run_command` so automation can display the
canonical direct command while executing the standard workflow entrypoint. `zv
check --format json` returns the combined skills/workflows/docs contract
result. `zv workflows check` remains available as the workflow-scoped spelling.

Repo-local skills currently exposed through `zv skills`:

- `zackvideo-cs2-utility-shorts`
- `zackvideo-lineup-audit`
- `zackvideo-music-scripted-shorts`
- `zackvideo-shorts-production`
- `zackvideo-youtube-shorts-publish`

Legacy binaries remain supported through pass-through commands such as
`zv parser`, `zv editor`, `zv recorder`, `zv composer`, and `zv orchestrator`.

## CS2 market intelligence prototype

`services/cs2-market` is a separate Python service for public CS2 item market
research. It ingests free/public data, stores price snapshots, scores 2-8 week
small-ticket signals, and exports Shorts-ready scripts. It is intentionally
separate from the Go demo-to-video pipeline.

```powershell
cd services\cs2-market
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e .
cs2market init-db
cs2market ingest skinport --currency USD
cs2market ingest historical-csv data\market\historical\prices.csv --source legacy-prices
cs2market ml status --category skin
cs2market ml features --category skin --out data\market\ml\features.csv
cs2market ml labels --features data\market\ml\features.csv --horizon-days 30 --out data\market\ml\labels.csv
cs2market ml train --labels data\market\ml\labels.csv
cs2market score --limit 25
cs2market export-shorts
```

`zv demo parse` parses a demo without the orchestrator:

```bash
./bin/zv demo parse \
  --demo testdata/foo.dem \
  --steamid 76561198000000000 \
  --rules testdata/example.rules.json \
  --out plan.json --verbose
```

To list demo participants and find SteamID64 values for targeting:

```bash
./bin/zv demo players \
  --demo testdata/foo.dem
```

To create one segment per target-player smoke throw instead of kill windows:

```bash
./bin/zv demo parse \
  --demo testdata/foo.dem \
  --steamid 76561198000000000 \
  --segment-mode smokes \
  --out plan-smokes.json --verbose
```

`zv shorts render` generates one clean 9:16 short per recorded segment:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --killplan data/runs/plan.json \
  --out data/runs/run-004/shorts-natural-hq2-full \
  --preset natural-hq2-full
```

When `--killplan` is omitted, `zv-editor` tries to discover it from
`pipeline-result.json` next to the recording run. The editor also writes a
YouTube Shorts upload pack under `shorts/publish/` with clean MP4 filenames,
English caption files, local gameplay cover JPGs, `pack-manifest.json`, and an
`index.html` review gallery. Rendered shorts are checked as 1080x1920 H.264
60fps MP4s and warned if they exceed the current Shorts length limit of 180s.
Use `--no-covers` to skip JPG cover extraction. Use `--dry-run` to write the
planned manifests, captions, FFmpeg commands, cover paths, and GPT Image cover
prompts without rendering or publishing videos.
Use `--skip-existing` to reuse already rendered MP4/JPG files, and
`--open-gallery` to open `shorts/publish/index.html` after a successful run.
The gallery includes local search/filter controls and copy buttons for titles
and captions; `publish-summary.md` gives a compact upload table.
For music-scripted edits, use `--music`, `--rhythm`, `--fps 24`, and
`--compile-segments` to render selected segments as one compiled Short. Analyze
the CC0 music first with `zv music analyze --killplan <plan.json>` so Lua kill
effects and cuts use the compiled rhythm timeline.
The default x264 encode is `--video-crf 18 --video-preset fast`. For a larger,
cleaner local master, use a lower CRF and slower preset, for example
`--video-crf 16 --video-preset slow`.
Use `--preset natural-hq` for the preferred realistic export: no scripted
effects, x264 CRF 16, and x264 preset `slow`.
Use `--preset natural-hq2-full` for the current saved/recommended realistic
export: no scripted effects, full captured gameplay/UI preserved inside the
vertical Shorts canvas, a mild FFmpeg-only saturation lift for CS2-style digital
vibrance, x264 CRF 16, x264 preset `slow`, Lanczos scaling, square-pixel
normalization, audio loudness normalization, black/freeze quality checks, and
cover contact sheets.
Use `--preset natural-hq2` only when a vertical center crop is intentionally
preferred over preserving the complete HUD/radar/killfeed frame.
Use `--preset natural-hq2-full-plus` for A/B comparison renders with stronger
FFmpeg-only digital-vibrance color, light sharpening, CRF 15, x264 preset
`slower`, and BT.709 mastering metadata.
`natural-hq3` and `natural-hq3-smooth` remain experimental comparison presets;
`natural-hq2-full` is the baseline to keep unless a future comparison clearly
beats it.

The legacy `short-clean` default applies the built-in Lua effects preset that
reproduces the clean local look: subtle kill punch-ins and text labels. Current
CS2 kill/highlight production should pass `--preset natural-hq2-full` instead.
Use
`--effects-preset awpgod` for stronger punch-ins, color grade, and AWP flashes;
use `--effects-preset none` for a base vertical crop without scripted effects;
or pass a custom Lua script with `--effects`:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-awpgod \
  --effects-preset awpgod \
  --dry-run
```

`effects/awpgod.lua` contains the same idea as an editable starting point when
you want to tweak the Lua directly.

Lua effects run in the post-processing editor, not in HLAE. HLAE recording
still uses generated JavaScript. The current local Lua DSL exposes
`on_segment`, `on_kill`, `on_smoke`, `zoom`, `flash`, `text`, and `grade`.
Scripts run in a local sandbox with filesystem/process modules disabled, and
each short's Lua evaluation is capped so a bad loop cannot hang the editor.

For the current natural baseline:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-natural-hq \
  --preset natural-hq
```

For the saved HQ2 full-UI baseline:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-natural-hq2-full \
  --preset natural-hq2-full
```

For the sharper digital-vibrance comparison preset:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-natural-hq2-full-plus \
  --preset natural-hq2-full-plus
```

For smoke-lineup clips, parse with `--segment-mode smokes`, record the emitted
segments, then render with the educational preset. The preset keeps the
`natural-hq2` encode/features baseline and adds only restrained text overlays:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --killplan data/runs/plan-smokes.json \
  --out data/runs/run-004/shorts-smoke-lineups \
  --preset smoke-lineups \
  --lineup-catalog data/lineups
```

Lineup destinations come from manual JSON catalogs such as
`data/lineups/de_ancient.smokes.json`. If a smoke landing does not match the
catalog, the editor still creates the clip, labels it as unmatched, and writes
`unmatched-smokes.json` under the output directory for review.

For an experimental HQ3 comparison:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-natural-hq3 \
  --preset natural-hq3
```

For an experimental smoother-motion HQ3 comparison:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-natural-hq3-smooth \
  --preset natural-hq3-smooth
```

For fast visual iteration, render only selected segments or cap the pack size:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-preview \
  --segments seg-001,seg-004 \
  --limit 2 \
  --skip-existing \
  --open-gallery
```

For a player-branded layout, pass a transparent PNG cutout:

```bash
./bin/zv shorts render \
  --recording-result data/runs/run-004/recording/recording-result.json \
  --out data/runs/run-004/shorts-premium \
  --preset short-premium-player \
  --player-image assets/players/martinez.png
```

If the source cutout still has a flat background, `--player-key-color #000000`
can chromakey it during FFmpeg composition. The source video remains gameplay;
the player asset is overlaid in the lower third and the top copy is generated
from segment metadata as a short play description such as
`2K con M4A1-S en de_ancient`.

## Tests

```bash
make test
# Repository and worker integration tests skip if Postgres / TEST_DEMO_PATH is unavailable.
```

`make test` also runs `zv check` so the repo-local skills, workflow
catalog, and workflow docs stay aligned with the unified CLI contract.

See [`docs/specs/`](./docs/specs/) for the specs and plans that produced this code.
