# HLAE Prototype — Operator Runbook

This directory contains the artifacts for the HLAE prototype sub-slice
(see [`../../docs/specs/2026-05-14-hlae-prototype.md`](../../docs/specs/2026-05-14-hlae-prototype.md)).

The four experiments validate the open HLAE questions before any Go
code is written for `zv-recorder`.

## Prerequisites (on the Windows PC)

| Software | Verify with                                                                                |
|----------|--------------------------------------------------------------------------------------------|
| CS2      | `Get-Item "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"` |
| HLAE     | `HLAE.exe` and `AfxHookSource2.dll` present in the same folder. Use a Source-2-capable release (2024+). |
| FFmpeg   | `ffprobe -version`                                                                         |
| Demo     | `lavked-vs-tnc-m2-nuke.dem` copied to a local Windows path.                                |

If anything is missing, install it before continuing.

## How to run an experiment

From a PowerShell prompt **inside this directory**:

```powershell
.\run-experiment.ps1 `
    -Experiment e1 `
    -Demo "C:\demos\lavked-vs-tnc-m2-nuke.dem" `
    -HlaeExe "C:\HLAE\HLAE.exe"
```

The runner:

1. Resolves the `.mirv` script for the experiment.
2. Validates paths.
3. Creates an output directory (default `$env:TEMP\zv-hlae\<experiment>\`).
4. Launches HLAE → CS2 with the `.mirv` preloaded.
5. Waits for CS2 to exit (triggered by the `quit` command in the script).
6. Prints wall-clock time and the contents of the output directory.

Run the four experiments in order (`e1` → `e4`).

## What to look at after each run

| Experiment | Files to inspect                          | Tool                      |
|------------|-------------------------------------------|---------------------------|
| e1         | Single `.mp4` (or TGA seq) in OutDir      | VLC + `ffprobe`           |
| e2         | Three `.mp4` files                        | VLC + `ffprobe` on each   |
| e3         | Whatever C1 produces; if nothing, edit the .mirv to use C2 and rerun | `ls`, `ffprobe`           |
| e4         | Single `.mp4`, compare with e1            | `ffprobe`, side-by-side   |

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
| `mirv_cmd: unknown command`                   | The `.mirv` syntax is wrong for this HLAE version. Note the error in the findings doc and stop — the spec will be updated before proceeding. |
| `mirv_streams add ffmpeg`: empty output (E3)  | Switch to C2 in `e3-output-format.mirv` (comment C1, uncomment C2), rerun. |
| Output `.mp4` is 0 bytes                      | Likely `mirv_streams record start` never fired; check the tick numbers. |
