# Toolchain

This repo is a deterministic CS2 demo-to-video pipeline. External tools are
allowed, but each one should have a clear boundary so renders can be reproduced.

## Core Rule

Keep gameplay capture and frame processing deterministic:

1. Parse the `.dem` into a kill/segment plan.
2. Record fixed windows from CS2/HLAE with fixed resolution, FPS, HUD mode, CRF,
   and tick margins.
3. Normalize every segment through FFmpeg into the same master shape.
4. Add overlays from event JSON as a separate transparent layer.
5. Encode the final upload file from those stable inputs.

AI-assisted or design-heavy tools can help generate assets and templates, but
they should not change the parsed events, recording windows, or final timing.

## Required Runtime Tools

| Tool | Role | Notes |
| --- | --- | --- |
| Go | Builds the parser, recorder, composer, editor, workers, and API. | Keep CLI behavior testable through subprocess-style tests. |
| Docker | Local Postgres + Redis for the orchestrator. | Parser-only work does not require it. |
| CS2 | Replays the `.dem` for recording. | Close any already-running CS2 process before HLAE capture. |
| HLAE | Launches CS2 with the Source 2 hook and records gameplay windows. | Use HLAE 2.x with `AfxHookSource2.dll` available next to `HLAE.exe` or under `x64\`. |
| FFmpeg + ffprobe | Muxing, concat, crop, scale, audio normalization, overlay composition, and quality checks. | HLAE also needs FFmpeg configured separately. |

Windows capture defaults used during local validation:

```powershell
$env:ZV_HLAE_PATH = "C:\HLAE-2.190.1\HLAE.exe"
$env:ZV_CS2_PATH = "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"
$env:ZV_FFMPEG_PATH = "ffmpeg"
```

On this workstation, `C:\HLAE\HLAE.exe` is not the FragForge capture install.
Use the versioned `C:\HLAE-2.190.1\HLAE.exe` path explicitly.

Check the local machine:

```powershell
.\scripts\check-toolchain.ps1

# Treat HLAE/CS2 capture paths as required and also probe npx tools.
.\scripts\check-toolchain.ps1 -StrictCapture -CheckNpxPackages
```

Run the codebase fix loop:

```powershell
.\scripts\fix-loop.ps1
```

The loop applies `gofmt` when needed, then runs `go vet ./...`,
`go test ./... -count=1`, `zv check`, `scripts/build.ps1`, and checks
that generated video/demo files have not reappeared in the repo. Use
`-Toolchain` when you also want the non-strict local toolchain diagnostic in the
same pass.

HLAE FFmpeg config:

```ini
[Ffmpeg]
Path=C:\path\to\ffmpeg.exe
```

Write that as `C:\HLAE\ffmpeg\ffmpeg.ini`, or place FFmpeg under
`C:\HLAE\ffmpeg\bin\ffmpeg.exe`. The recorder tries to create the INI from
`PATH` when possible.

## Capture

Build all local CLIs on Windows:

```powershell
.\scripts\build.ps1
```

The recorder behind `zv record` owns HLAE/CS2. It generates deterministic HLAE
JavaScript and launches CS2 through HLAE:

```powershell
.\bin\zv.exe record `
  --demo testdata\match.dem `
  --killplan data\runs\plan.json `
  --out data\runs\recording `
  --hlae $env:ZV_HLAE_PATH `
  --cs2 $env:ZV_CS2_PATH `
  --fps 120 `
  --video-crf 16
```

Use `--dry-run` when editing the generated HLAE script or validating paths
without launching the game.

For professional Shorts, prefer recording at `120` or `240` FPS, then producing
a deterministic 60 FPS delivery file. This gives post-processing enough samples
for light temporal cleanup while the final output stays compatible with YouTube
Shorts.

## FFmpeg Normalization

The current proven vertical baseline is:

- `tmix=frames=2:weights='1 1'` for very light deterministic temporal cleanup.
- `fps=60` for the delivery cadence.
- restrained `eq` contrast/saturation.
- scale wide, center crop to `1080x1920`.
- `loudnorm` plus `aresample=48000`.
- `blackdetect` and `freezedetect` as post-render checks.

Keep all segment outputs at the same resolution, FPS, SAR, pixel format, and
audio sample rate before concatenation.

## Lua Effects

Lua runs in `zv-editor` post-processing, not in HLAE. Use it for deterministic
timeline decisions such as small zooms, text annotations, flashes, grades, and
segment-specific labels.

Text, image, and killfeed overlays can declare `fade_in` and `fade_out` seconds
to avoid hard cuts at 24 FPS and 60 FPS delivery rates.

Use Lua when the effect is logic-driven and tied to kill metadata. Avoid using
Lua for polished motion graphics that are easier to author in an animation
timeline; use HyperFrames for that layer instead.

## HyperFrames Overlays

HyperFrames fits the overlay layer: kill counters, streak numbers, lower thirds,
event labels, and other animated UI that should sit above gameplay.

Recommended boundary:

1. Generate an overlay event file from the kill plan.
2. Render the HyperFrames composition at the final output resolution and FPS.
3. Export with transparency.
4. Composite the transparent overlay over the normalized gameplay master with
   FFmpeg.

Local validation showed:

- `npx --yes hyperframes lint <project>` is useful before rendering.
- `--format mov` produced ProRes 4444 with alpha that FFmpeg can composite.
- `--format webm` was not reliable for our Windows/FFmpeg alpha path in the
  tested setup.

Example:

```powershell
npx --yes hyperframes render overlays\hyperframes\kill-number-probe `
  --format mov `
  --fps 60 `
  --resolution 1080x1920 `
  --output data\overlay.mov
```

Then composite:

```powershell
ffmpeg -i gameplay-master.mp4 -i data\overlay.mov `
  -filter_complex "[1:v]format=yuva444p[ov];[0:v][ov]overlay=0:0:format=auto,format=yuv420p[v]" `
  -map "[v]" -map 0:a -c:v libx264 -crf 18 -preset slow -c:a copy final.mp4
```

Do not commit rendered overlay MOV/WebM/PNG sequences. Commit only the
composition source and event-template code.

Font note: do not vendor Windows system fonts for portable repo assets. For a
committed HyperFrames template, use a redistributable font such as Inter, Roboto,
or another font with a compatible license.

## OpenSrc

`opensrc` is a research tool for dependency internals. Use it when docs are not
enough and we need to inspect real package source:

```powershell
npx --yes opensrc path hyperframes
rg "alpha" (npx --yes opensrc path hyperframes)
```

It stores source in a global cache, not in this repo. Keep it as an agent/developer
tool, not a runtime dependency.

## Python Scripts

Python is acceptable for one-off analysis, visualization, and asset generation
scripts such as tactical replay experiments. Keep the production render path in
Go + FFmpeg unless a Python library clearly owns the problem better.

Good Python candidates:

- tactical map previews from exported event JSON;
- offline quality analysis;
- future music/beat analysis with `librosa`;
- frame-diagnostic tools that do not belong in the runtime worker.

If a Python script becomes part of production, add a pinned environment file and
document expected inputs/outputs.

## GPT Image / Asset Generation

Use image generation for optional visual assets such as covers, player cutouts,
backgrounds, or overlay design references. Generated assets must be treated as
inputs, not hidden pipeline behavior:

- store prompts or prompt templates when they affect reproducibility;
- commit only small reusable assets that are legally safe to keep;
- keep final video timing and event data independent from generated imagery.

## Cleanup Policy

The repo should not keep generated media:

- `data/runs/`, `data/tries/`, `data/demos/`, `bin/`, and local `.dem` fixtures
  are disposable.
- `data/lineups/*.json` can be committed when it is hand-curated reference data.
- HyperFrames source projects can be committed when they are small and portable;
  rendered overlay files should stay ignored.

Use the cleanup script for known experiment directories, and delete ignored
media folders directly when a run has already been uploaded or archived.

## Failure Checks

Before accepting a generated Short:

```powershell
ffprobe -v error -select_streams v:0 `
  -show_entries stream=width,height,sample_aspect_ratio,display_aspect_ratio,r_frame_rate,duration `
  -show_entries format=size,duration `
  -of default=noprint_wrappers=1 final.mp4

ffmpeg -v error -i final.mp4 `
  -vf "blackdetect=d=1:pix_th=0.10,freezedetect=n=-60dB:d=2" `
  -an -f null -
```

For Shorts, expected output is normally `1080x1920`, `9:16`, `60 fps`, H.264
video, AAC audio, and no black/freeze warnings.
