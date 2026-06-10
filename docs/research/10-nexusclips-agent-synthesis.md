# Nexus Clips agent synthesis for FragForge

Date: 2026-06-03

Input material:

- `docs/research/09-nexusclips-public-recon.md`
- `docs/specs/2026-06-03-allstar-inspired-product-iteration.md`
- Existing FragForge architecture and code contracts
- Four read-only agent reviews: product/workflow, local AI, editor/render,
  and security/operation

## Executive Decision

Use Nexus Clips as a workflow reference, not as an implementation template.

The valuable pattern is:

`source -> analysis -> moments -> edit document -> render variant -> publish pack`

For FragForge this should stay local-first and CS2-specific:

`demo -> killplan -> moments -> recording -> render variant -> shortslistosparasubir`

The next product layer should introduce three durable contracts:

1. `Moment`: a scored, explainable candidate derived from `killplan.Segment`.
2. `EditDocument`: a stable, user/editable render intent without absolute
   paths or FFmpeg commands.
3. `RenderVariant`: the materialized state and artifacts for one output
   variant such as `natural-hq2-full`.

Do not make `editor.Manifest` the public edit document. It is a compiled render
plan with local paths, artifacts, and commands. Keep it as an execution
artifact produced from an `EditDocument`.

## What Applies

### Moment Before Clip

Nexus separates markers/moments from rendered clips. FragForge should do the
same. The parser already produces strong raw material: kills, utility throws,
 rounds, ticks, map, target player, weapons, headshots, wallbangs, and smoke
lineup metadata.

`Moment` should be a reviewable product object, not a replacement for
`killplan.Segment`.

Recommended fields:

- `schema_version`
- `job_id`
- `id`
- `source`
- `segment_id`
- `round`
- `tick_start`
- `tick_end`
- `duration_seconds`
- `score`
- `reason_codes`
- `events`
- `warnings`
- `default_variant`

Useful reason codes:

- `multi_kill`
- `headshot`
- `wallbang`
- `awp`
- `rifle`
- `pistol`
- `utility_lineup`
- `known_lineup`
- `unmatched_lineup`
- `short_duration`
- `long_duration`
- `round_importance_unknown`
- `recording_missing`

### Render Variant As A First-Class Object

The repo already has a partial foundation:

- `tasks.TypeRenderVariant`
- `tasks.RenderVariantPayload`
- `artifacts.RenderVariant*Key`

Complete this before adding heavier AI. It is the nearest useful slice because
it wraps existing `zv-editor` behavior and makes output state durable.

Recommended state document:

- `job_id`
- `variant`
- `status`: `queued`, `rendering`, `ready`, `failed`
- `preset`
- `edit_document_key`
- `edit_manifest_key`
- `render_result_key`
- `pack_manifest_key`
- `gallery_key`
- `artifact_prefix`
- `warnings`
- `error`
- `created_at`
- `updated_at`

### Edit Document As Stable Intent

Nexus exposes an editor model with subtitles, hook, sticker, framing, and
watermark layers. FragForge should adopt the data-contract idea, not the heavy
browser editor.

Recommended `EditDocument` fields:

- `schema_version`
- `job_id`
- `variant`
- `created_at`
- `source`
- `selection`
- `loadout_snapshot`
- `layers`
- `publish`
- `outputs`

`selection` can start with ordered `segment_ids`. Later it can accept
`moment_ids`.

`loadout_snapshot` should freeze all values that affect output:

- preset
- effects preset
- CRF
- x264 preset
- HQ filters
- audio normalize
- quality checks
- cover sheets
- covers enabled

`layers` should be conservative at first:

- framing: default full UI for `natural-hq2-full`
- subtitles: disabled unless generated/imported
- hook: optional text metadata, not rendered by default
- sticker/image: disabled for baseline
- watermark: disabled for upload-ready packs
- cover: generated from existing cover-time logic

### Local Publish Pack

Nexus has publication/calendar flows. FragForge should copy the operational
status model, not direct cloud publishing.

Keep the upload-ready default under `shortslistosparasubir` and track:

- ready video
- cover
- caption
- hashtags/title
- pack manifest
- gallery
- review/summary
- status: `draft`, `ready`, `needs_cover`, `needs_caption`, `uploaded`,
  `failed`

### Codex CLI As Control Plane

Codex CLI should not be the durable backend or frame processor. Go, Asynq,
FFmpeg, HLAE, and deterministic tools should own execution.

Good Codex CLI uses:

- generate caption/title candidates from `moments.json` and `pack-manifest.json`
- explain why moments were selected
- summarize QA warnings
- propose a loadout from structured context
- produce review notes in JSON with a strict schema

Avoid using Codex CLI for:

- process supervision
- queue durability
- rendering
- parsing `.dem`
- validating security or permissions
- reading secrets or local credentials

## What Not To Copy

Do not copy:

- cloud-first video uploads as the core product
- generic "clip any video" positioning
- social feed/profile/ads mechanics
- OAuth publishing as a required early path
- Canva-style editor complexity
- opaque AI marker generation without reason codes
- public sourcemaps or frontend-visible backend logic

FragForge's advantage is CS2 demo intelligence: ticks, POV, HUD preservation,
weapons, rounds, kills, utility, HLAE capture, and repeatable local renders.

## Implementation Order

### 1. Finish `render:variant`

Goal: render one named variant through existing `zv-editor` and store all
artifacts under `jobs/{id}/renders/{variant}`.

Work:

- Add missing artifact keys:
  - `jobs/{id}/moments/moments.json`
  - `jobs/{id}/renders/{variant}/edit-document.json`
  - `jobs/{id}/renders/{variant}/edit-manifest.json`
  - `jobs/{id}/renders/{variant}/publish-summary.md`
  - `jobs/{id}/renders/{variant}/logs/{name}.log`
- Implement `RenderWorker.HandleRenderVariant`.
- Add orchestrator config:
  - `ZV_EDITOR_PATH`
  - `ZV_RENDER_TIMEOUT`
  - `ZV_FFPROBE_PATH`
- Register the worker in `zv-orchestrator`.
- Add API:
  - `POST /api/jobs/{id}/renders/{variant}`
  - `GET /api/jobs/{id}/renders/{variant}`
- Use idempotency/locking or `asynq.Unique` so concurrent renders do not
  overwrite the same variant.

For v1, map `variant=natural-hq2-full` directly to:

- `editor.PresetShortNaturalHQ2Full`
- `EffectsPresetNone`
- default full UI framing
- existing cover/caption/pack generation

### 2. Add `internal/moments`

Goal: derive explainable moments from `killplan.Plan` without recording,
FFmpeg, HLAE, Whisper, or Codex.

Work:

- Add `internal/moments` types and scoring.
- Build from `killplan.Segment`.
- Emit stable `moments.json`.
- Add `GET /api/jobs/{id}/moments`.
- Keep generation synchronous until scoring becomes expensive.

Initial scoring should be deterministic:

- kill count
- headshot count
- wallbang count
- primary weapon
- segment duration
- utility type
- known lineup confidence
- warnings for missing metadata

### 3. Add Loadout Catalog

Goal: promote existing editor presets into stable product choices.

Start with code-backed loadouts:

- `natural-hq2-full`
- `natural-hq2-full-plus`
- `smoke-lineups`

Each loadout should define preset, effects, quality flags, cover behavior,
caption behavior, and output shape. The render worker should snapshot the
loadout into `EditDocument` for reproducibility.

### 4. Add Local Publish Board

Goal: copy the useful part of Nexus publication without platform API risk.

Expose a local status document around `shortslistosparasubir`:

- render ready
- cover present
- captions present
- gallery path
- manifest path
- manual uploaded flag
- last validation warnings

This can start as JSON plus gallery before any richer UI.

### 5. Add Optional Media QA

Goal: improve confidence after rendering.

Use local deterministic tools first:

- ffprobe: duration, FPS, codec, resolution
- FFmpeg filters: blackdetect, freezedetect, loudnorm stats
- contact sheets: already aligned with existing editor behavior

Use OpenCV later for visual cover quality, bad-frame detection, or HUD/killfeed
checks only if FFmpeg is insufficient.

### 6. Add Optional Codex Agent Tasks

Goal: structured editorial assistance.

Create a narrow `agent:codex` task only after the deterministic contracts are
stable. Inputs should be JSON, outputs should be JSON schema, and the worker
should run with:

- no secret access
- read-only context
- timeout
- small prompt
- stored result artifact

Good first task: caption/title variants from `moments.json` and
`pack-manifest.json`.

### 7. Add UI Last

Goal: make the first screen a workbench, not marketing.

The first useful UI should show:

- match/job list
- parsed moments
- selected moments
- recording status
- render variants
- publish pack readiness
- gallery/open-folder links

Avoid a generic editor until the contracts prove stable.

## Security And Operations Guardrails

Lessons from Nexus public sourcemaps:

- Never publish production `.map` files by accident.
- Add CI/build checks for sourcemaps and obvious secrets in public assets.
- Upload sourcemaps privately to an error tracker only if needed.
- Frontend state is advisory; backend validates transitions and permissions.

Local API defaults:

- Prefer `127.0.0.1:8080` over `:8080` for future UI/API defaults.
- Allow LAN binding only through explicit opt-in and warning.
- Deny CORS by default.
- Add a local mutation token before exposing UI controls beyond localhost.
- Never accept raw paths or command arguments from the UI.
- Validate UUIDs, variants, artifact names, sizes, content types, and status
  transitions server-side.

Telemetry should be local JSONL, not third-party analytics by default:

- request id
- job id
- task type
- transition
- duration
- artifact keys
- process exit code
- retry count
- validation warnings
- failure reason

Redact env vars, tokens, credential URLs, and sensitive paths from exportable
diagnostics.

## Near-Term Recommendation

The next coding iteration should be:

1. Complete `render:variant` end to end.
2. Add artifact keys for edit document, edit manifest, render result, logs, and
   publish summary.
3. Add `RenderWorker` using existing `zv-editor`.
4. Add minimal render endpoints.
5. Then add `internal/moments`.

This order produces value quickly, stays deterministic, and avoids premature AI
or UI complexity.
