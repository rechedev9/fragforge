# Recorder Low-Level Plan

**Date:** 2026-05-15
**Status:** implemented as local foundation; HLAE prototype gate passed.
**Component:** `zv-recorder` local Windows CLI.

## Goal

Create the bottom-up recording foundation before orchestrator integration:

1. Convert parser kill plans into a typed local `RecordingPlan`.
2. Generate deterministic HLAE 2.x JavaScript from that plan.
3. Provide a local `zv-recorder` CLI that can dry-run without CS2 and launch HLAE when real paths are provided.
4. Emit `recording-result.json` as the artifact contract for later worker/orchestrator integration.

## Ground Truth From Prototype

HLAE 2.x `mirv_script_load` runs Boa JavaScript, not `.mirv` text commands. The carrier must use:

- `mirv.events.clientFrameStageNotify.on(...)`
- `mirv.getDemoTick()`
- `mirv.exec(...)`
- one-shot guards around each scheduled command

Commands must fire on `tick >= target`, not `tick == target`.

The working CS2 recording path is:

- `mirv_streams record fps 60`
- `mirv_streams record screen enabled 1`
- `mirv_streams record screen settings afxFfmpegYuv420p`
- seek 2 seconds before `record start`
- `spec_mode 1; spec_player_by_accountid <id>` 1 second before `record start`
- `demoui` shortly before `record start`

## Implemented Files

- `internal/recording/types.go` - recording contract, validation, SteamID64 to AccountID conversion.
- `internal/recording/scriptgen.go` - deterministic HLAE JavaScript generation.
- `cmd/zv-recorder/main.go` - local CLI with `--dry-run` and HLAE launch path.
- `scripts/hlae/e*.js` - JS prototype scripts replacing broken `.mirv` files.
- `scripts/hlae/run-experiment.ps1` - renders JS with absolute output dir and waits on CS2.

## Local CLI Contract

```powershell
zv-recorder `
  --killplan C:\path\killplan.json `
  --demo C:\path\demo.dem `
  --out C:\path\recordings\job-id `
  --hlae C:\HLAE\HLAE.exe `
  --cs2 "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"
```

Dry-run:

```powershell
zv-recorder --dry-run --killplan C:\path\killplan.json --demo C:\path\demo.dem --out C:\tmp\zv-out
```

Dry-run writes:

- `recording.js`
- `recording-result.json`

## Validation Completed

- `go test ./... -count=1` passes on Windows with Go 1.26.3.
- `zv-recorder --dry-run` successfully generates `recording.js` and `recording-result.json`.
- `scripts/hlae/run-experiment.ps1` parses as PowerShell.
- E1 produced a clean 1920x1080 60fps POV clip.
- E2 produced three independent playable takes in one CS2 session.
- E3 produced H.264 `video.mp4` plus WAV `audio.wav`.
- E4 produced matching frames/duration with `host_timescale 2`, but no material wall-clock gain on the short clip.
- `internal/recording.CollectArtifacts` discovers `takeNNNN` folders, maps them to segment IDs, and probes video/audio metadata with `ffprobe`.
- `zv-recorder` checks HLAE FFmpeg configuration before launch and writes `ffmpeg.ini` from `PATH` when possible.
- `internal/recording.MuxSegmentClips` combines `takeNNNN/video.mp4` and `audio.wav` into `segments/<segment-id>.mp4` using FFmpeg.

## Remaining Work

- Define the next composition stage for effects, overlays, music, and final concat.
