---
name: zackvideo-stream-clips
description: "Create upload-ready FragForge stream clips from recorded stream VODs: pick a layout variant, plan the edit, import reviewed killfeed and Spanish captions, and render vertical or landscape packs after the user approves the creative brief."
---

# FragForge Stream Clips

Use this skill when the user wants clips from a recorded stream VOD (Twitch/YouTube/local MP4), especially vertical facecam-over-gameplay Shorts or landscape long-form cuts with factual killfeed notices and Spanish captions.

The journey is `stream variants -> stream plan -> stream killfeed -> stream captions -> stream render`.
Reviewed word timings keep captions independent of cloud credentials; `zv stream transcribe` can generate local unreviewed candidates when no reviewed timings exist yet.

## Creative Brief Gate

Before any non-dry-run render, ask the user only for the creative choices they have not already supplied, grouped into one concise message, and wait for explicit approval:

- delivery/layout variant: discover the supported list first and offer the real names (`streamer-vertical-stack-40-60` facecam stack, `streamer-fullframe-nocam`, `streamer-landscape-16x9`, plus any newly listed variant);
- clip boundaries: which stream moments to cut (start/end timestamps or clip IDs) and one title per clip;
- killfeed treatment: factual killfeed notices on or off, and the reviewed events source when on;
- captions: Spanish captions on or off, and whether reviewed word timings exist or local transcription candidates must be generated and reviewed first;
- music: none, or a track directory the user provides;
- delivery shape: one clip per moment or one longer compilation.

If the user delegates creative control, state the resolved defaults and treat that delegation as approval.
Preserve every approved answer in the exact render argv; do not silently replace answers with preset defaults later.

## Workflow

1. Discover layout variants and default geometry:

```powershell
.\bin\zv.exe workflows run stream-variants -- --format json
```

2. Plan the edit for the chosen variant and clip boundaries:

```powershell
.\bin\zv.exe workflows run stream-plan -- `
  --input <stream.mp4> `
  --variant streamer-vertical-stack-40-60 `
  --clip-id <clip-01> `
  --clip-start <hh:mm:ss> `
  --clip-end <hh:mm:ss> `
  --title "<clip title>" `
  --detect-killfeed `
  --captions `
  --out <run>\edit-plan.json
```

Use `--dry-run` first when iterating on crops or clip boundaries.

3. Import reviewed factual killfeed notices when the user approved a killfeed:

```powershell
.\bin\zv.exe workflows run stream-killfeed -- `
  --plan <run>\edit-plan.json `
  --events <run>\killfeed-events.json `
  --out <run>\reviewed-plan.json
```

Killfeed notices must stay factual: only kills that are visible in the clip, never invented events.

4. Generate local transcription candidates when captions are approved but no reviewed word timings exist yet:

```powershell
.\bin\zv.exe workflows run stream-transcribe -- `
  --input <stream.mp4> `
  --plan <run>\reviewed-plan.json `
  --model <models>\ggml-large-v3.bin `
  --vad-model <models>\ggml-silero-vad.bin `
  --out <run>\transcript-review.json
```

The candidates are explicitly unreviewed; have the user review and correct them before the import step.
Skip this step when reviewed word timings already exist.

5. Import reviewed Spanish word timings when the user approved captions:

```powershell
.\bin\zv.exe workflows run stream-captions -- `
  --plan <run>\reviewed-plan.json `
  --words <run>\caption-words.json `
  --out <run>\captioned-plan.json
```

6. Render the approved plan:

```powershell
.\bin\zv.exe workflows run stream-render -- `
  --input <stream.mp4> `
  --plan <run>\captioned-plan.json `
  --out <run>
```

Run with `--dry-run` to show the resolved plan before the real render, and only remove it after the Creative Brief Gate is approved.

## QA

- Verify each output with `ffprobe`: H.264 video, AAC audio, `1080x1920` for vertical variants or `1920x1080` for `streamer-landscape-16x9`, and a nonzero duration.
- Confirm the facecam and gameplay crops match the plan and nothing important (killfeed, HUD, facecam) is cut off.
- Confirm captions are readable, correctly timed, and in Spanish, and that killfeed notices match real kills in the clip.
- Put upload-ready MP4s, captions, manifests, and review assets under `<run>\shortslistosparasubir`.
- Point the user to the `shortslistosparasubir` folder when delivering finished media.
