# HLAE Prototype — Findings

**Run date:** 2026-05-15  
**HLAE:** 2.190.0 with `AfxHookSource2.dll` from `C:\HLAE\x64\`  
**CS2 build in console:** build `10772`, revision `10658053`  
**Demo:** `testdata/lavked-vs-tnc-m2-nuke.dem`  
**Target:** `maaryy`, SteamID64 `76561198148986856`, account id `188721128`

## Verdict

The recorder path is viable with HLAE Source 2, but it must use the screen-recording form of `mirv_streams`, not the old CS:GO named stream shape.

Working setup:

```text
mirv_streams record name "<out>/<experiment>-rec"
mirv_streams record fps 60
mirv_streams record screen enabled 1
mirv_streams record screen settings afxFfmpegYuv420p
spec_show_xray 0; cl_drawhud 0
demo_gototick <segment_start - 128>
spec_mode 1; spec_player_by_accountid 188721128
demoui
mirv_streams record start
...
mirv_streams record end
```

Important details:

- HLAE does not use `ffmpeg.exe` from `PATH` automatically. It needs `C:\HLAE\ffmpeg\bin\ffmpeg.exe` or `C:\HLAE\ffmpeg\ffmpeg.ini`.
- `mirv_streams record fps 60` is required before `record start`.
- Seek must land before the record start. A 2-second seek lead and 1-second camera lead fixed the spectator camera race.
- `spec_mode 1; spec_player_by_accountid <accountID>` works once the lead time exists.
- `demoui` must run after the seek and shortly before `record start`; running it too early leaves the demo transport bar in the recording.

## Results

| Experiment | Result | Wall-clock | Output |
|---|---:|---:|---|
| E1 seek/camera | PASS | 31.44 s | 1 POV clip, clean UI |
| E2 multi-segment | PASS | 83.27 s | 3 takes in one CS2 session |
| E3 output format | PASS | 36.20 s | H.264 MP4 video + separate WAV audio |
| E4 host_timescale 2 | PASS with caveat | 31.35 s | Same frames/duration as E1; no useful wall-clock gain on short clip |

Video probes:

| File | Codec | Resolution | FPS | Duration | Frames |
|---|---|---:|---:|---:|---:|
| E1 `take0000/video.mp4` | h264 | 1920x1080 | 60/1 | 5.016667 | 301 |
| E2 `take0000/video.mp4` | h264 | 1920x1080 | 60/1 | 12.233333 | 734 |
| E2 `take0001/video.mp4` | h264 | 1920x1080 | 60/1 | 8.016667 | 481 |
| E2 `take0002/video.mp4` | h264 | 1920x1080 | 60/1 | 8.016667 | 481 |
| E3 `take0000/video.mp4` | h264 | 1920x1080 | 60/1 | 8.016667 | 481 |
| E4 `take0000/video.mp4` | h264 | 1920x1080 | 60/1 | 5.016667 | 301 |

Audio probes:

| File | Codec | Rate | Channels | Duration |
|---|---|---:|---:|---:|
| E1 `take0000/audio.wav` | pcm_s16le | 44100 | 2 | 5.015510 |
| E2 `take0000/audio.wav` | pcm_s16le | 44100 | 2 | 12.225306 |
| E2 `take0001/audio.wav` | pcm_s16le | 44100 | 2 | 8.010884 |
| E2 `take0002/audio.wav` | pcm_s16le | 44100 | 2 | 8.010884 |
| E3 `take0000/audio.wav` | pcm_s16le | 44100 | 2 | 8.010884 |
| E4 `take0000/audio.wav` | pcm_s16le | 44100 | 2 | 5.015510 |

## Decisions

- Use one CS2 session per demo and multiple `record start/end` windows.
- Default to HLAE screen recording with `afxFfmpegYuv420p`.
- Treat HLAE output as `{takeNNNN/video.mp4, takeNNNN/audio.wav}` and mux/composite later.
- Keep `host_timescale` default at `1` for now. It did not break recording, but the short-clip benchmark showed no meaningful end-to-end speedup.
- Make the recorder launch CS2 at the requested resolution (`-w 1920 -h 1080`) because HLAE captures the active CS2 viewport.

## Remaining Work

- Decide how to name or map HLAE `takeNNNN` folders back to segment IDs in `zv-recorder`.
- Add artifact discovery/probing/muxing to `zv-recorder`.
- Add an explicit config check for HLAE FFmpeg before launching CS2.
