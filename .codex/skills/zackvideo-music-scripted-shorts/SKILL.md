---
name: zackvideo-music-scripted-shorts
description: "Create Lua-scripted, music-synced FragForge CS2 Shorts: parse demos, record target-player segments, analyze CC0 music, render one 24fps compiled vertical Short per demo with external music, rhythm JSON, effects/viral_premium.lua, publish assets, gallery review, and upload-ready outputs under shortslistosparasubir."
---

# FragForge Music Scripted Shorts

Use this skill when the user wants CS2 Shorts with Lua scripting, external music, beat/rhythm sync, 24 FPS output, or one music-edited compilation per demo. Keep the normal realistic `natural-hq2-full` skill as the default for non-musical gameplay exports.

## Defaults

- Output shape: one compiled vertical Short per demo.
- Visual style: `--preset short-clean --effects effects\viral_premium.lua`.
- Final FPS: `--fps 24`.
- Music policy: CC0 only. Do not use royalty-free, Pixabay Content License, CC-BY, NC, or unclear-license tracks unless the user explicitly changes the policy.
- Default track source: OpenGameArt `Melodic EDM Loops` by Fupi, CC0, 140 BPM.
- Default file: `https://opengameart.org/sites/default/files/brightmelodicskippyedm.wav`.
- Store final upload-ready MP4s, covers, captions, manifests, and review notes under `<run>\shortslistosparasubir\...`.

## Workflow

Parse the target player's segments:

```powershell
.\bin\zv.exe workflows run demo-parse -- `
  --demo <demo.dem> `
  --steamid <SteamID64> `
  --out <run>\plan.json `
  --verbose
```

If the SteamID64 is unknown, list players:

```powershell
.\bin\zv.exe workflows run demo-players -- --demo <demo.dem>
```

Record with HLAE/CS2 only when the user has authorized capture:

```powershell
.\bin\zv.exe workflows run record -- `
  --killplan <run>\plan.json `
  --demo <demo.dem> `
  --out <run>\recording-gameplay-120 `
  --hlae C:\HLAE-2.190.1\HLAE.exe `
  --cs2 "<cs2.exe>" `
  --hud gameplay `
  --fps 120 `
  --video-crf 16 `
  --timeout 45m
```

Download the CC0 music into `<run>\music` and keep a small local note with the source page, direct file URL, author, title, and license. Do not commit the downloaded audio.

Analyze the music against the kill plan before rendering:

```powershell
.\bin\zv.exe workflows run music-analyze -- `
  --input <run>\music\brightmelodicskippyedm.wav `
  --killplan <run>\plan.json `
  --out <run>\rhythm.json `
  --min-bpm 130 `
  --max-bpm 150 `
  --kill-offset-ms 100 `
  --max-beats 512
```

Render the compiled music Short:

```powershell
.\bin\zv.exe workflows run shorts-render -- `
  --recording-result <run>\recording-gameplay-120\recording-result.json `
  --killplan <run>\plan.json `
  --out <run>\shorts-scripted-music `
  --publish-dir <run>\shortslistosparasubir\scripted-music `
  --preset short-clean `
  --effects effects\viral_premium.lua `
  --music <run>\music\brightmelodicskippyedm.wav `
  --rhythm <run>\rhythm.json `
  --fps 24 `
  --compile-segments `
  --video-crf 16 `
  --video-preset slow `
  --hq-filters `
  --audio-normalize `
  --quality-checks `
  --cover-sheets
```

Open the gallery for review:

```powershell
.\bin\zv.exe workflows run gallery-open -- --path <run>\shortslistosparasubir\scripted-music\index.html
```

## QA

- Verify exactly one compiled publish MP4 per demo unless the user requested a different shape.
- Confirm `ffprobe` reports H.264, AAC audio, `1080x1920`, and `24/1` or equivalent frame rate.
- Confirm Lua text/flash/zoom effects land on kill moments in the compiled timeline.
- Confirm music is present, game audio is audible but secondary, and no section is silent unless it is an intentional rhythm gap.
- Confirm `pack-manifest.json`, `publish-summary.md`, captions, covers, and review assets are under `shortslistosparasubir`.
