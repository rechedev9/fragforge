# Local Pipeline Plan

**Date:** 2026-05-15  
**Status:** implemented and validated against a full 9-segment demo.  
**Component:** `zv-pipeline` local CLI.

## Goal

Provide one local command that chains the two verified media stages:

1. `zv-recorder` records all kill-plan segments with HLAE/CS2.
2. `zv-composer` concatenates segment clips into `final.mp4`.
3. `zv-pipeline` writes `pipeline-result.json` with step durations and output paths.

This is still local-machine orchestration, not the distributed server/worker flow.

## CLI

```powershell
zv-pipeline `
  --killplan C:\path\killplan.json `
  --demo C:\path\demo.dem `
  --out C:\path\job-output `
  --hlae C:\HLAE\HLAE.exe `
  --cs2 "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"
```

Optional:

- `--recorder C:\path\zv-recorder.exe`
- `--composer C:\path\zv-composer.exe`
- `--ffmpeg C:\path\ffmpeg.exe`
- `--record-timeout 20m`
- `--compose-timeout 20m`

If `--recorder` / `--composer` are omitted, the pipeline looks for sibling
executables next to `zv-pipeline`, then falls back to `PATH`.

## Output Layout

```text
job-output/
  pipeline-result.json
  final.mp4
  composition-result.json
  concat-list.txt
  recording/
    recording-result.json
    recording.js
    take0000/
    ...
    segments/
      seg-001.mp4
      ...
```

## Validation Completed

Input:

- `testdata/lavked-vs-tnc-m2-nuke.expected.json`
- `testdata/lavked-vs-tnc-m2-nuke.dem`

Output root:

- `%TEMP%\zv-pipeline-real-full`

Result:

| Field | Value |
|---|---|
| Pipeline error | empty |
| Recorder duration | 217.93 s |
| Composer duration | 28.48 s |
| Recording warnings | 0 |
| Recording artifacts | 27 |
| Composition warnings | 0 |
| Final video | H.264, 1920x1080, 60/1 fps, 4732 frames |
| Final audio | AAC, 44100 Hz, stereo |
| Final duration | 78.922132 s |
| Final size | 184056031 bytes |

## Orchestrator Integration Added

- `internal/artifacts` defines durable storage keys:
  - `jobs/{id}/recording/recording-result.json`
  - `jobs/{id}/recording/recording.js`
  - `jobs/{id}/recording/segments/{segment_id}.mp4`
  - `jobs/{id}/composition/composition-result.json`
  - `jobs/{id}/composition/final.mp4`
- `record:demo` worker materializes the demo + kill plan, runs `zv-recorder`, uploads the recording result, script, and segment MP4s, then marks the job `recorded`.
- `compose:final` worker localizes stored segment MP4s, runs `zv-composer`, uploads `composition-result.json` and `final.mp4`, then marks the job `composed`.
- `zv-orchestrator` registers media workers only when the relevant env vars are set, so API/parser-only development still works without HLAE.
- HTTP gates:
  - `POST /api/jobs/{id}/record`
  - `POST /api/jobs/{id}/compose`
  - `GET /api/jobs/{id}/final`

## Remaining Work

- Add cleanup policy for raw takes after final delivery.
- Add effects/music/overlays between segment mux and final concat.
