# Allstar-inspired product iteration

Date: 2026-06-03

This note captures public-product lessons from Allstar and translates them into
FragForge slices. It is not a plan to clone Allstar. The goal is to keep
FragForge's deterministic CS2 pipeline while borrowing the product primitives
that make automated clips reusable.

## Observed public product primitives

- Match history is the user's working surface, not a raw upload queue.
- A parsed match exposes moments before clips exist.
- Clip creation is a request against a moment, with visible progress and retry.
- Desktop and mobile outputs are separate render variants.
- Studio cards/loadouts are reusable bundles of effects, overlays, music, and
  publishing choices.
- Publish packs include more than MP4: cover, caption, gallery, and metadata.
- Profiles, feeds, reactions, and ads are secondary to the core clip lifecycle.

## FragForge target model

```
Match
  source demo/archive/share-code metadata
  map, teams, players, parsed_at

Moment
  match_id
  segment_id
  player, round, tick range
  kills/smokes/events
  eligibility and warnings

RenderVariant
  moment_id or job_id
  variant name: final, natural-hq2-full, mobile, publish-pack
  preset/loadout snapshot
  status, progress, artifacts

Loadout
  stable user-facing name
  editor preset
  effects preset/script
  output aspect/FPS/quality
  publishing defaults
```

The current `Job` and `KillPlan` remain valid. Jobs are still the execution
unit; matches, moments, variants, and loadouts are product-level views on top of
the same parser/recorder/editor contracts.

## Iteration slices

1. Render artifact contracts.
   Add stable storage keys under `jobs/{id}/renders/{variant}/...` so the
   orchestrator can address future short/mobile/publish outputs without
   changing the existing recording and composition paths.

2. Render variant task type.
   Add an Asynq task for rendering a named variant from an existing recording
   result. The first variant should wrap the existing `zv-editor` publish-pack
   output, not introduce new FFmpeg behavior.

3. Variant status API.
   Expose `GET /api/jobs/{id}/renders/{variant}` and
   `POST /api/jobs/{id}/renders/{variant}`. The response should show status,
   pack manifest, gallery URL, and downloadable artifacts when present.

4. Moment library API.
   Expose parsed segments as moments with enough metadata for a frontend table:
   player, map, round, tick/time range, kill count, weapons, smoke destination,
   and warnings.

5. Loadout catalog.
   Promote the current editor presets into a small loadout catalog. Keep the
   catalog code-backed at first; user-created loadouts can come after the
   frontend proves the workflow.

6. Match history page.
   Build the first frontend screen around matches and moments, not marketing.
   It should show parsed demos, detected moments, render status, and direct
   actions for recording/rendering/downloading.

## Non-goals for the next iteration

- Social profiles, comments, reactions, feeds, and follows.
- Ads and public content discovery.
- Multi-game support.
- Mobile app distribution.
- A marketplace for user-generated effects.

Those can be valuable later, but they do not improve the core CS2 render loop
yet.
