# HLAE Prototype Operator Runbook

This directory contains the artifacts for the HLAE prototype sub-slice
(see [`../../docs/specs/2026-05-14-hlae-prototype.md`](../../docs/specs/2026-05-14-hlae-prototype.md)).

The four experiments validate the open HLAE questions before `zv-recorder`
is wired into the orchestrator.

## Prerequisites (on the Windows PC)

| Software | Verify with                                                                                |
|----------|--------------------------------------------------------------------------------------------|
| CS2      | `Get-Item "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"` |
| HLAE     | `HLAE.exe` present, plus `AfxHookSource2.dll` either next to `HLAE.exe` (older releases) or under `x64\` (HLAE 2.x). Use a Source-2-capable release (2024+). |
| FFmpeg   | `ffprobe -version`; HLAE also needs `C:\HLAE\ffmpeg\bin\ffmpeg.exe` or `C:\HLAE\ffmpeg\ffmpeg.ini`. |
| Demo     | `lavked-vs-tnc-m2-nuke.dem` copied to a local Windows path.                                |

If anything is missing, install it before continuing.

## How to run an experiment

From a PowerShell prompt **inside this directory**:

```powershell
.\run-experiment.ps1 `
    -Experiment e1 `
    -Demo "C:\demos\lavked-vs-tnc-m2-nuke.dem" `
    -HlaeExe "C:\HLAE-<version>\HLAE.exe"
```

The runner:

1. Resolves the `.js` script for the experiment.
2. Validates paths.
3. Creates an output directory (default `$env:TEMP\zv-hlae\<experiment>\`).
4. Renders a per-run JS script into `OutDir`.
5. Launches HLAE → CS2 with that JavaScript preloaded via `mirv_script_load`.
6. Waits for the matching `cs2.exe` process to exit.
7. Prints wall-clock time and the contents of the output directory.

**Run order:** start with **E3** if changing HLAE versions, because it validates the recording output format. The old `.mirv` carrier was removed: HLAE 2.x loads JavaScript, so the experiments are now `e1`-`e4` `.js` files using `mirv.events.clientFrameStageNotify`, `mirv.getDemoTick()`, and `mirv.exec(...)`.

Recommended order: **E3 → E1 → E2 → E4**.

## What to look at after each run

| Experiment | Files to inspect                          | Tool                      |
|------------|-------------------------------------------|---------------------------|
| e1         | `e1-rec\take0000\video.mp4` and `audio.wav` | VLC + `ffprobe`           |
| e2         | Three `takeNNNN` folders, each with `video.mp4` and `audio.wav` | VLC + `ffprobe` on each   |
| e3         | `e3-rec\take0000\video.mp4` and `audio.wav` | `ls`, `ffprobe`           |
| e4         | `e4-rec\take0000\video.mp4`, compare with e1 | `ffprobe`, side-by-side   |

`ffprobe` cheat sheet:

```powershell
ffprobe -v error -show_entries format=duration,nb_streams `
        -show_streams -of default=noprint_wrappers=1 path\to\file.mp4
```

## How to record findings

1. Copy `docs/research/07-hlae-prototype-results.md.template` to
   `docs/research/07-hlae-prototype-results.md`.
2. Fill in the tables for each experiment as you go.
3. Upload one representative `.mp4` (preferably from e2) to a shared
   storage and link it in the findings file.
4. Commit the filled-in findings file. **Do not commit the `.mp4`s
   themselves** — they are too big and not reproducible from the repo.

## Troubleshooting

| Symptom                                       | Action                                                          |
|-----------------------------------------------|-----------------------------------------------------------------|
| `AfxHookSource2.dll not found`                | Put it next to `HLAE.exe` (it ships in the HLAE release).        |
| CS2 launches but the demo never starts        | The HLAE version may be too old for the demo protocol. Update.  |
| Script loads but no scheduled actions fire     | Confirm the rendered `*.rendered.js` exists in `OutDir` and contains `mirv.events.clientFrameStageNotify`. |
| `Failed writing image for screen recording`    | Configure HLAE FFmpeg: create `C:\HLAE\ffmpeg\ffmpeg.ini` with `[Ffmpeg]` and `Path=<absolute ffmpeg.exe>`. The runner does this automatically when possible. |
| Output is 1280x960 or another unexpected size  | Pass `-Width 1920 -Height 1080`; HLAE captures the active CS2 viewport. |
| Recording starts from free camera              | Keep the 2-second seek lead before `record start`; camera changes need time after `demo_gototick`. |
| Demo transport bar appears in the clip         | Ensure `demoui` runs after the seek and shortly before `record start`, not only at script setup. |
| E3 ran but OutDir looks empty                  | Inspect `OutDir\e3.rendered.js`; HLAE writes under `<record name>\takeNNNN\`. |
| Output `.mp4` is 0 bytes                      | `mirv_streams record start` never fired. Check the tick numbers and confirm a stream is configured before the record window. |
