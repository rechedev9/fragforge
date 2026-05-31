# Zack Replay Shorts

Produce one vertical CS2 Short per demo from `C:\Users\reche\Downloads\replays`, focused on every kill by Zack. The production path should be deterministic, rhythm-aware, and traceable through ZackVideo manifests and logs.

## Decision

Use the existing ZackVideo pipeline, with a small renderer extension so Shorts can consume external music, 24 FPS output, and `zv-rhythm` beat timing.

## Inputs

| Topic          | Decision                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Demos          | All `.dem` files in `C:\Users\reche\Downloads\replays`                                                                   |
| Player         | Zack, `SteamID64 76561197997743909`                                                                                      |
| Output shape   | One Short per demo                                                                                                       |
| Kill scope     | Include all kills by Zack from each demo                                                                                 |
| Music          | `Trap Loop 130bpm` by SpatelyK4 from Freesound/Openverse: `https://cdn.freesound.org/previews/490/490790_9818679-hq.mp3` |
| Music license  | CC0 1.0                                                                                                                  |
| Video rate     | 24 FPS final output                                                                                                      |
| Visual effects | Lua effects from `effects/viral_premium.lua`                                                                             |
| Capture        | HLAE/CS2 authorized, using `C:\HLAE-2.190.1\HLAE.exe`                                                                    |

## Architecture

The production run should keep `cmd/` entrypoints thin and place behavior in existing internal packages.

| Area     | Design                                                                                                                                                 |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Parser   | Use existing demo parsing with Zack's SteamID64 to build one kill plan per demo.                                                                       |
| Recorder | Use existing HLAE/CS2 recording flow to produce segment clips and `recording-result.json`.                                                             |
| Rhythm   | Download the CC0 music file once, run `zv music analyze`, and generate rhythm JSON with beat timestamps plus segment sync for each per-demo kill plan. |
| Editor   | Extend the editor config/manifest/FFmpeg command generation to accept a music path, rhythm path, output FPS, and per-demo compilation mode.            |
| Lua      | Keep visual timing driven by kill cues. Lua overlays emphasize kill impacts, combos, final count, and Zack branding.                                   |
| Outputs  | Write all artifacts under `data/runs/zack-replays-YYYYMMDD/`, with one child directory per demo.                                                       |

## Data Flow

1. Discover demos in `C:\Users\reche\Downloads\replays`.
2. For each demo, run demo player/parse steps for `76561197997743909`.
3. Record all planned Zack kill segments with HLAE/CS2.
4. Analyze `Trap Loop 130bpm` through `zv-rhythm`.
5. Build editor manifests that include the music source, rhythm analysis path, FPS `24`, Lua effects path, and all Zack kill segments for that demo.
6. Render one publishable compilation Short per demo instead of one output per segment.
7. Generate publish packs, captions, covers, gallery pages, and logs.

## Rhythm Sync

The current rhythm package can produce `segment_sync`, but the editor does not yet consume it. The renderer extension should map rhythm timing into the final edit rather than relying on manual FFmpeg post-processing.

For each Short, Zack's kill cue should land close to a music beat or a fixed offset after the beat. The initial target is beat plus 100 ms.

If a demo has too many kills for a single pass of the music file, the music may loop. Looping must preserve beat continuity so later kills still align to the 130 BPM grid.

## FFmpeg Behavior

The editor should support these new render controls without changing default behavior when omitted:

| Control     | Behavior                                                                                                                |
| ----------- | ----------------------------------------------------------------------------------------------------------------------- |
| Output FPS  | Final video filter and encode output should be 24 FPS for this run. Existing defaults remain unchanged for other runs.  |
| Music input | Mix music-forward audio deterministically: music at volume `1.0`, game audio at volume `0.20` when source audio exists. |
| Rhythm JSON | Used to place cuts/timeline offsets so kills align to beat timing.                                                      |

## Error Handling

Fail fast for missing demo files, missing HLAE/CS2 paths, missing FFmpeg, missing music file, invalid rhythm JSON, or an empty Zack kill plan.

Record non-fatal per-demo render issues in the result JSON and continue to the next demo only when the failure is isolated to one demo. Do not hide HLAE, FFmpeg, or parser errors; preserve logs under the run directory.

## Testing

Add unit tests for the renderer changes without invoking HLAE, CS2, or long media renders.

Required coverage:

- Editor config validation accepts optional music/rhythm/FPS fields.
- Default editor behavior remains unchanged when new fields are omitted.
- FFmpeg command generation includes 24 FPS when configured.
- FFmpeg command generation includes external music input and deterministic music-forward audio mapping when configured.
- Rhythm JSON mapping rejects missing or malformed sync data with useful errors.

Manual verification is required for the final production outputs because HLAE/CS2 capture and FFmpeg renders are media workflows.

## Out Of Scope

- UI changes.
- Redis/orchestrator changes.
- Database schema changes.
- Publishing uploads to YouTube or TikTok.
- Manually selecting only the best kills; this run includes all Zack kills per demo.

## Acceptance Checklist

- [ ] Eight demos are discovered from `C:\Users\reche\Downloads\replays`.
- [ ] Each demo produces one Short centered on Zack's kills.
- [ ] Each Short uses 24 FPS final output.
- [ ] Each Short uses the CC0 `Trap Loop 130bpm` track.
- [ ] Rhythm analysis is generated with `zv-rhythm`/`zv music analyze`.
- [ ] Lua effects are present and timed from kill cues.
- [ ] Publish packs, captions, covers, manifests, and logs are written under `data/runs/zack-replays-YYYYMMDD/`.
- [ ] Unit tests cover renderer config and FFmpeg command changes.
- [ ] Project gate passes before declaring implementation complete.

## Next Step

After this spec is approved, create an implementation plan that first adds the renderer controls and tests, then runs the authorized capture/render production workflow for the eight demos.
