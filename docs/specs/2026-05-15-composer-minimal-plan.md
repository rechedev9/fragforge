# Composer Minimal Plan

**Date:** 2026-05-15  
**Status:** implemented as first local concat slice.  
**Component:** `zv-composer` local CLI.

## Goal

Create the smallest useful post-recording stage after `zv-recorder`:

1. Read `recording-result.json`.
2. Select `role=segment`, `type=video` artifacts in kill-plan order.
3. Concatenate them into a single `final.mp4`.
4. Emit `composition-result.json` for the next pipeline stage.

This slice intentionally does not implement effects, beat sync, music, overlays, or final delivery.

## CLI

```powershell
zv-composer `
  --recording-result C:\path\recording-result.json `
  --out C:\path\final.mp4
```

Optional:

- `--ffmpeg C:\path\ffmpeg.exe`
- `--timeout 20m`
- `--dry-run`

## Output

- `final.mp4`
- `concat-list.txt`
- `composition-result.json`

The current FFmpeg path uses concat demuxer plus re-encode:

- H.264 video
- CFR 60 fps
- yuv420p
- AAC audio at 192 kbps
- `+faststart`

## Validation Completed

Test input: full 9-segment `zv-recorder` output in `%TEMP%\zv-recorder-real-full`.

Final probe:

| Field | Value |
|---|---|
| Duration | 79.133107 s |
| Video | h264, 1920x1080, 60/1 fps, 4743 frames |
| Audio | aac, 44100 Hz, stereo |
| Size | 183645091 bytes |

## Remaining Work

- Add per-segment effects before concat.
- Add music/beat sync and ducking.
- Add overlays/titles/watermark.
- Decide whether the full media composer remains Go+FFmpeg CLI or moves to Python/ffmpeg-python for complex filtergraph generation.
