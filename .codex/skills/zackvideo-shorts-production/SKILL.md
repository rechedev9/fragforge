---
name: zackvideo-shorts-production
description: "Generate professional CS2 Shorts with FragForge from demos, archives, kill plans, utility plans, or recording results. Use when asked to create, rerender, review, polish, concatenate, split, or QA Shorts packs, especially viral-60-clean POV kill highlights with deathnotices, viral-ultra-clean overlays, 120fps capture, long compilations, map-specific compilations, split by map/player, joining many Shorts into one video, publish galleries, and platform-ready upload assets."
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
- Use the latest official HLAE release for local capture. Confirm the highest installed version with `zv capabilities --format json`, compare it with the latest official AdvancedFX release, and update before capture when necessary.
- Let FragForge auto-detect the highest numeric version under `C:\HLAE-*\HLAE.exe`; use `--hlae` only for an explicit override. Never use `C:\HLAE\HLAE.exe`.
- CS2 must launch through HLAE in windowed mode for recording runs. The CS2
  command line must include `-windowed`; do not record demos in fullscreen or
  borderless fullscreen.
- Use `--dry-run` before changing recording settings or when only inspecting planned commands/manifests.
- Do not run HLAE/CS2 or long renders unless the user explicitly wants a capture/render run.

## Creative Brief Gate

Before non-dry-run capture or render, ask only the creative choices the user
has not already answered. Group them into one concise request and use the CLI's
supported choices:

- delivery: `short-9x16` or `landscape-16x9`;
- game presentation: `gameplay` (full HUD), `deathnotices`, or `clean`, with a readable factual
  killfeed;
- kill effect: `clean`, `punch-in`, `velocity`, or `freeze-flash`;
- transition: `cut`, `flash`, `whip`, or `dip`;
- kill numbering/counter: disabled or enabled; enabled includes the built-in
  milestone labels such as 2K/3K/ACE;
- intro/outro text and music;
- thumbnail generation: gameplay cover candidates or no cover. Custom designed
  cover text is not part of the current render CLI contract and must not be
  promised as a render option.

Do not ask again for decisions already present in the request. Do not treat
ambiguous execution words like "go", "hazlo", "dale", "ok", or "ya deberia
estar ok" as approval unless they answer a previously shown brief. If the user
delegates creative control before a brief exists, state the resolved defaults as
a concrete brief and ask for approval; only a follow-up confirmation approves
the run. Otherwise wait for explicit approval, then preserve every answer in
the exact dry-run and real render argv.

Use this question shape for a fresh demo request:

```text
Antes de capturar/renderizar dime/confirmame:
1. Formato: short-9x16 o landscape-16x9.
2. Jugador y seleccion: SteamID/jugador, todas las kills, mejores momentos, o elijo tras enseñarte ranking.
3. Presentacion: gameplay HUD completo, deathnotices, o clean.
4. Estilo: kill effect, transicion, contador/milestones, intro/outro.
5. Audio: sonido original, musica, o ambos.
6. Subtitulos/textos: si/no y que textos quieres quemar.
7. Thumbnail: candidates de gameplay o sin cover.
```

After rendering cover candidates, show the cover sheet or candidate images and
ask the user to select the final thumbnail. The pack is not upload-ready until
the user selects one or explicitly delegates automatic selection.

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

## Selection Gate

When the user wants to choose which kills or plays make the final video, stop at the selection gate before any capture.
Score the planned segments, show the ranked moments, and ask the user which segments to keep and in what order:

```powershell
.\bin\zv.exe workflows run demo-moments -- `
  --killplan testdata/agent-killplan.json `
  --format json
```

Build the recorder-ready plan from the approved selection, preserving the user's requested narrative order.
Point `--killplan` at the run's parsed plan, list the approved segment IDs in the approved order, and remove `--dry-run` to write the selected plan:

```powershell
.\bin\zv.exe workflows run demo-select -- `
  --killplan testdata/agent-killplan.json `
  --segments seg-001 `
  --out data\runs\agent-doc\selected-plan.json `
  --dry-run `
  --format json
```

Skip this gate only when the user already fully specified the target and selection policy, for example "all kills" for one SteamID64.
Record from the selected plan when the gate ran; never silently record segments the user filtered out.

## Utility Audit

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
  --out <run>\recording-deathnotices-120 `
  --hud deathnotices `
  --fps 120 `
  --video-crf 16 `
  --timeout 45m
```

For standard kill Shorts, record with clean/deathnotice HUD and high source FPS so the editor keeps a clear POV while the kill notices narrate the frags. Use `--hud deathnotices` unless the user explicitly asks for full gameplay HUD. For a dry run, keep the same command shape but use `--dry-run` and omit `--hlae`/`--cs2`.

The recorder adds `-windowed` to the CS2 command line passed through HLAE. If a manual HLAE launch is ever needed, include `-windowed` with the existing `-w 1920 -h 1080` flags.

If CS2 crashes near the end, inspect `<run>\recording-...\segments` and `recording-result.json` before rerunning. Compare segment MP4 count against the plan, verify every listed segment with `ffprobe`, and confirm `recording-result.json` references those files. Continue from existing artifacts when the check passes; rerun only missing or corrupt segments when it fails.

## Standard Render

Use `viral-60-clean` as the current professional baseline and standard preset for kill/highlight Shorts. It is the latest designed default: clean HUD-less 60fps POV with kill notices, viral-ultra-clean overlays, hook text, punch-ins, kill counters, CRF 16 slow encode, Lanczos scaling, square-pixel normalization, audio loudness normalization, black/freeze checks, and cover sheets.

Do not add `--temporal-smoothing`; the 120fps source already downsamples cleanly to 60fps and frame blending can create ghosting on fast camera/weapon movement.

For standard viral kill clips, use the same render workflow with `--preset viral-60-clean`, `--compile-segments`, the deathnotice-HUD recording result, and an output such as `<run>\shorts-viral60-clean-<player>-<map>`. For fast iteration, add `--segments seg-001,seg-004 --limit 2 --dry-run`. Use `--skip-existing` only when burned-in video content will not change.

```powershell
.\bin\zv.exe workflows run shorts-render -- `
  --recording-result <run>\recording-deathnotices-120\recording-result.json `
	--killplan <run>\plan.json `
	--out <run>\shorts-viral60-clean-<player>-<map> `
	--publish-dir <run>\shortslistosparasubir `
	--preset viral-60-clean `
  --compile-segments `
  --video-crf 16 `
  --video-preset slow `
  --hq-filters `
  --audio-normalize `
  --quality-checks `
  --cover-sheets
```

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
.\bin\zv.exe workflows run gallery-open -- --path <run>\shortslistosparasubir\index.html
```

Check the gallery and `publish-summary.md` for:

- 1080x1920 vertical framing with clean POV/deathnotice presentation for `viral-60-clean`.
- The killfeed/deathnotices, crosshair, weapon view, and round context visible in the POV remain readable enough to represent the demo correctly.
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
