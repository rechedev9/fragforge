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

**Run order:** start with **E3** to confirm which `mirv_streams add` syntax actually works in this HLAE version. Then, before running E1, E2, or E4, copy the working `mirv_streams add ffmpeg main "..."` (or `mirv_streams add tga main`) line from your edited `e3-output-format.mirv` into each of the other three `.mirv` files, scheduled at tick 25 (i.e. `mirv_cmd add tick 25 "mirv_streams add ..."`). E1/E2/E4 only call `mirv_streams record start`; they assume the stream is already configured.

Recommended order: **E3 → E1 → E2 → E4**.

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
| E1/E2/E4 produce no output at all              | You probably skipped configuring a stream. E3 discovers the working `mirv_streams add` syntax — run E3 first, then paste the working line into the other `.mirv` files at tick 25 before running them. |
| E3 ran but OutDir looks empty                  | `e3-out.mp4` uses a relative path and is written to CS2's working directory, not `OutDir`. Search the CS2 install folder (e.g. `C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\` and parent folders) for `e3-out.mp4`. Once found, document its absolute path in the findings. The follow-up `zv-recorder` spec will fix this by passing an absolute output path. |
| Output `.mp4` is 0 bytes                      | `mirv_streams record start` never fired. Check the tick numbers and confirm a stream is configured before the record window. |
