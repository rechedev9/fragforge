---
name: zackvideo-shorts-production
description: "Generate professional CS2 Shorts with FragForge from demos, archives, kill plans, utility plans, or recording results. Use when asked to create, rerender, review, polish, concatenate, split, or QA Shorts packs, especially realistic full-gameplay kill highlights with complete CS2 UI, FFmpeg-only natural-hq2-full rendering, mild digital-vibrance saturation, 120fps capture, long compilations, map-specific compilations, split by map/player, joining many Shorts into one video, publish galleries, and platform-ready upload assets."
---

# FragForge Shorts Production

## Overview

Use FragForge as the deterministic source of truth: parse demo segments, record with HLAE/CS2, render vertical Shorts with the editor, then review the publish gallery. Prefer code/preset improvements and reproducible FFmpeg output before introducing manual NLE steps.

## Before Running

- Run from the repository root.
- Check `git status --short` before edits. If the tree is dirty, report the relevant state, preserve unrelated changes, and avoid touching files already modified by someone else unless the task requires it.
- Keep every run in an explicit run-specific output directory, preferably under `data/runs/...` or `data/tries/...`.
- Treat `data/` and rendered media as output unless the task explicitly asks to clean or inspect artifacts.
- Do not stage or commit generated MP4/WAV/PNG/WebP/JPG/HTML publish artifacts, local binaries, or capture output.
- Do not clean, delete, or overwrite existing run artifacts unless the user explicitly asks.
- Use `C:\HLAE-2.190.1\HLAE.exe` for local capture. Do not use `C:\HLAE\HLAE.exe`.
- Before any non-dry-run capture, verify `Test-Path C:\HLAE-2.190.1\HLAE.exe` and stop if any command references `C:\HLAE\HLAE.exe`.
- CS2 must launch through HLAE in windowed mode for recording runs. The CS2
  command line must include `-windowed`; do not record demos in fullscreen or
  borderless fullscreen.
- Use `--dry-run` before changing recording settings or when only inspecting planned commands/manifests.
- Do not run HLAE/CS2 or long renders unless the user explicitly wants a capture/render run.

## Inputs

- Parse the demo into a segment plan. For utility Shorts, add `--segment-mode utility` and write `plan-utility.json`. For kill highlight packs, add `--rules <run>\all-kills.rules.json`.

```powershell
.\bin\zv.exe workflows run demo-parse -- `
  --demo <demo.dem> `
  --steamid <SteamID64> `
  --out <run>\plan.json `
  --verbose
```

- If the SteamID64 is unknown, list players:

```powershell
.\bin\zv.exe workflows run demo-players -- --demo <demo.dem>
```

- For utility/lineup Shorts, audit labels after parsing:

```powershell
.\bin\zv.exe workflows run utility-audit -- `
  --plan <run>\plan-utility.json `
  --lineup-catalog data\lineups `
  --out <run>\utility-audit.csv
```

Treat `catalog` destinations as publishable. Treat `auto` and `unknown` as review candidates before burning text into overlays.

For kill highlight packs, prefer an explicit "all kills" rules file so pistols, SMGs, rifles, knives, and utility kills are not silently filtered by the parser default weapon list. Use the previous local `all-kills.rules.json` shape when it exists in a comparable run; otherwise create one under the run directory with every canonical weapon name from `internal/parser/demo.go` and `exclude_team_kills: true`.

## Record

Generate the HLAE script first. Remove `--dry-run` only when the user wants an actual capture run.

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

For realistic kill Shorts, record with the full gameplay HUD visible and high source FPS so the editor preserves radar, killfeed, score, crosshair, health, ammo, and round context while delivering smoother 60fps output. Use `--hud gameplay` unless the user explicitly asks for a cleaner/deathnotice-only style. For a dry run, keep the same command shape but use `--dry-run` and omit `--hlae`/`--cs2`.

The recorder adds `-windowed` to the CS2 command line passed through HLAE. If a manual HLAE launch is ever needed, include `-windowed` with the existing `-w 1920 -h 1080` flags.

If CS2 crashes near the end, inspect `<run>\recording-...\segments` and `recording-result.json` before rerunning. Compare segment MP4 count against the plan, verify every listed segment with `ffprobe`, and confirm `recording-result.json` references those files. Continue from existing artifacts when the check passes; rerun only missing or corrupt segments when it fails.

## Legacy Player-Branded Render

Do not use this older blurred/player-photo/Lua style by default. It remains here only for explicit user requests that ask for player branding, blurred top/bottom bands, custom overlays, or Lua effects.

Use this pattern only when the user provides an explicit player image path or explicitly approves a discovered image. Do not guess which image belongs to a player. If no suitable image is available, ask for one or fall back to `natural-hq2-full` rather than burning in the wrong asset.

It matches the older branded style: blurred top/bottom gameplay, centered square gameplay, player cutout/photo in the lower band, channel logo watermark in the upper-right blurred band, native deathnotice killfeed in the upper right, mild saturation, and smoother 60fps delivery.

1. Convert player images and the channel logo to PNG assets under `<run>\assets` when needed. The channel logo watermark is required for the standard player-branded style; use the user-provided channel logo image, not a guessed replacement.

```powershell
ffmpeg -y -v error -i "<player-image.webp>" <run>\assets\<player>-player.png

ffmpeg -y -v error -i "<channel-logo>" `
  -vf "scale=132:-1:flags=lanczos,format=rgba,colorchannelmixer=aa=0.82" `
  <run>\assets\channel-logo-watermark.png
```

Before rendering, inspect the player image enough to confirm it matches the target player, has usable framing, and has transparency or a keyable background. Inspect the logo asset enough to confirm it is the user's channel logo and remains readable at watermark size. After rendering, check the gallery that the player image stays in the lower blurred band, the logo is anchored at the top right, and neither overlay covers the core gameplay or killfeed.

2. Create a small Lua effects file per player:

```lua
local player_asset = "<run>/assets/<player>-player.png"
local channel_logo = "<run>/assets/channel-logo-watermark.png"

on_segment(function(s)
  grade({
    contrast = 1.03,
    saturation = 1.22,
    gamma = 1.00
  })

  image({
    path = player_asset,
    start = 0,
    duration = s.duration,
    x = "(W-w)/2",
    y = "H-h+16",
    width = 430
  })

  image({
    path = channel_logo,
    start = 0,
    duration = s.duration,
    x = "W-w-24",
    y = 24,
    width = 132
  })
end)

on_kill(function(k)
  killfeed({
    at = k.time,
    pre = 0.35,
    post = 2.80,
    x = "W-w-18",
    y = 438,
    width = 430,
    crop_x = 1558,
    crop_y = 64,
    crop_width = 360,
    crop_height = 110
  })
end)
```

3. Render with `viral-square` and the quality flags:

```powershell
.\bin\zv.exe workflows run shorts-render -- `
  --recording-result <run>\recording-deathnotices-120\recording-result.json `
  --killplan <run>\plan.json `
  --out <run>\shorts-<map>-<player>-photo-killfeed `
  --preset viral-square `
  --effects <run>\<player>-photo-killfeed.lua `
  --video-crf 16 `
  --video-preset slow `
  --hq-filters `
  --audio-normalize `
  --quality-checks `
  --cover-sheets `
  --temporal-smoothing
```

Use `--dry-run` first when changing the Lua, crop, player image, or output directory. Do not use `--skip-existing` for burned-in visual changes.

Expected warnings: source recordings at `120/1` may warn that the source is not 60fps; that is acceptable when the rendered publish MP4s verify as `1080x1920` and `60/1`.

## Natural/Utility Render

Use `natural-hq2-full` as the current professional baseline for kill/highlight Shorts. It is FFmpeg-only, avoids Lua/scripted effects, uses one continuous 9:16 gameplay crop with no black bars and no stacked foreground/background bands, applies a mild saturation lift for a digital-vibrance feel, and keeps CRF 16 slow encode, Lanczos scaling, square-pixel normalization, audio loudness normalization, black/freeze checks, and cover sheets.

Do not add `--temporal-smoothing` for realistic gameplay exports unless explicitly A/B testing it. The 120fps source already downsamples cleanly to 60fps; frame blending can create ghosting on fast camera/weapon movement.

Use `natural-hq2-full-plus` only for explicit A/B tests. It keeps the same full-UI layout but adds stronger digital-vibrance color, light sharpening, CRF 15, x264 preset `slower`, and BT.709 mastering metadata.

For natural kill clips, use the same render workflow with `--preset natural-hq2-full`, the gameplay-HUD recording result, and an output such as `<run>\shorts-natural-hq2-full`. For utility clips, use `--killplan <run>\plan-utility.json --preset smoke-lineups --lineup-catalog data\lineups`. For fast iteration, add `--segments seg-001,seg-004 --limit 2 --dry-run`. Use `--skip-existing` only when burned-in video content will not change.

For kill/highlight deliverables, treat the per-segment rendered Shorts as intermediate publish inputs. After the MP4s pass validation, concatenate all selected kills for each player/game into one long vertical upload-ready Short by default. Deliver individual per-kill Shorts only when the user explicitly asks for separate Shorts.

## Long Compilations

When asked to join many Shorts into one longer vertical video, do not use `xfade` for FragForge compilation renders; use concat only. Use publish MP4s from `pack-manifest.json` or `publish-summary.md`, sorted by pack order/filename. Do not join raw segment MP4s or unrelated preview files.

Before joining, run `ffprobe` on every input and reject missing audio, non-H.264 video, unexpected resolution/FPS, or corrupt files. For large joins, create a short sample or extract review frames before delivering the final.

Use a clean concat graph and reencode once to normalize timestamps, video FPS, sample rate, and audio:

```powershell
# Build inputs from publish MP4s sorted by filename, then use:
ffmpeg -y -v error <inputs...> `
  -filter_complex "<per-input setpts/asetpts/fps/aresample>; <all labels> concat=n=<N>:v=1:a=1[vcat][acat]; [vcat]fps=60,format=yuv420p[v]; [acat]aresample=48000[a]" `
  -map "[v]" -map "[a]" `
  -c:v libx264 -preset slow -crf 16 `
  -c:a aac -b:a 192k -movflags +faststart `
  <out>.mp4
```

For splitting one combined request by map/player, generate separate outputs from the original per-map publish MP4s rather than cutting a previously concatenated file.

## Split By Map/Player

When the user asks for separate long videos by map or player:

- Group inputs from the original per-map/per-player publish directories, not from an already combined MP4.
- Preserve source order by `pack-manifest.json` item order or sorted publish filename.
- Name outputs explicitly, for example `<player>-<map>-all-kills-long.mp4` or `<player>-<map1>-<map2>-all-kills-long.mp4`.
- Write outputs to a new run-specific directory such as `<run>\combined-long-shorts-vN-<purpose>`.
- Verify each output independently with video and audio probes before opening or delivering it.

## Review

Open the generated gallery when it was not opened by the render command:

```powershell
.\bin\zv.exe workflows run gallery-open -- --path <out>\publish\index.html
```

Check the gallery and `publish-summary.md` for:

- 1080x1920 vertical framing with complete gameplay/UI preserved for `natural-hq2-full`.
- HUD, radar, killfeed, score, crosshair, health, ammo, and round context remain visible and readable enough to represent the demo correctly.
- No blurred top/bottom bands, player image overlays, logo overlays, cinematic crops, or Lua/scripted effects are present unless the user explicitly requested that style.
- Saturation looks slightly more vivid than stock CS2 capture without crushing detail or misrepresenting the gameplay.
- No black/frozen sections, missing audio, text clipping, or overlay overlap.
- Kill labels, lineup destination, origin, stance, and action match the plan/audit.
- Captions and titles are human-readable, specific, and not hashtag spam.
- Covers are usable at phone size.

Verify representative outputs with:

```powershell
ffprobe -v error -select_streams v:0 `
  -show_entries stream=codec_name,width,height,avg_frame_rate `
  -show_entries format=duration `
  -of default=nw=1 <video.mp4>

ffprobe -v error -select_streams a:0 `
  -show_entries stream=codec_name,sample_rate,channels `
  -of default=nw=1 <video.mp4>
```

If the issue is metadata only, edit/rerender with `--skip-existing` when appropriate. If overlay text, crop, timing, effects, or quality changes, rerender without `--skip-existing`.
