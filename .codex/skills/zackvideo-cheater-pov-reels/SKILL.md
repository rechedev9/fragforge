---
name: zackvideo-cheater-pov-reels
description: "Create CS2 Shorts reels against suspected cheaters or hackers from FragForge demos: every time the target is killed, show the killer POV first and then the target POV dying, concatenate all deaths into one long vertical video, and QA every kill block before delivery."
---

# FragForge Cheater POV Reels

Use this skill when the user wants a funny CS2 video against a suspected cheater/hacker, especially requests like "cada vez que lo matamos", "sale el POV de quien lo mata y luego su POV muriendo", or "video del hacker".

Pair this with `zackvideo-shorts-production` for the normal recording/rendering pipeline.

## Creative Brief Gate

Before any non-dry-run capture or render, ask the user only for the creative choices they have not already supplied, grouped into one concise message, and wait for explicit approval:

- delivery format: vertical `short-9x16` reel (default) or `landscape-16x9`;
- HUD treatment: full `gameplay` HUD (default for this skill so both POVs are readable) or `deathnotices`;
- kill effect on each death block: `clean`, `punch-in`, `velocity`, or `freeze-flash`;
- block labels: the killer/target label wording, or the suggested defaults below;
- kill numbering: `D01`, `D02`, ... counters on or off;
- intro/outro text: none (default) or user-provided wording;
- music: none (default) or a user-provided track;
- thumbnail strategy: generated gameplay cover candidates or no cover.

The transition between blocks is fixed by the reel shape (concat only, no `xfade`), so do not offer it as a choice.

If the user delegates creative control, state the resolved defaults and treat that delegation as approval.
Preserve every approved answer in the exact recording and render argv; do not silently replace them with preset defaults later.
After cover candidates exist, show them and ask the user to choose the final thumbnail before calling the pack upload-ready, unless the user delegated that choice.

## Required Shape

The final video is one long vertical Short-style reel, never separate per-kill Shorts unless explicitly requested.

For every target death, the sequence must be:

```text
D01 killer POV: our player kills the target
D01 target POV: suspected cheater dies
D02 killer POV: our player kills the target
D02 target POV: suspected cheater dies
...
```

The killer block must be the actual killer's POV. The target/death block must be the target's POV. Do not deliver if any block shows the wrong player.

## Workflow

1. Identify the target:

```powershell
.\bin\zv.exe workflows run demo-players -- --demo <demo.dem>
```

Prefer exact `name_in_demo` plus SteamID64. If the target name is fuzzy, list likely matches and confirm only if ambiguity would risk recording the wrong player.

2. Extract target deaths:

- Include only deaths where the suspected cheater is the victim.
- Exclude suicides and team kills unless the user explicitly wants them.
- Keep utility kills; a molotov/grenade death still gets killer POV then target POV.
- Sort by tick and number as `D01`, `D02`, etc.
- Store a review JSON in the run folder, for example `death-events-<target>.json`, with tick, round, killer name/SteamID, target name/SteamID, weapon, headshot, and team.

3. Build two POV plans per death:

- Killer segment: target the killer, starting roughly 8 seconds before death and ending 4 seconds after death.
- Target segment: target the suspected cheater, using the same death window.
- Use stable segment IDs: `d01-killer`, `d01-target`, `d02-killer`, `d02-target`, etc.
- Use exact in-demo names for camera switching when SteamID/account-id switching fails or when prior QA shows the victim POV by mistake.

4. Confirm that `zv capabilities --format json` selects the latest official
   HLAE release, updating the local version if necessary, then record with
   gameplay HUD:

```powershell
.\bin\zv.exe workflows run record -- `
  --killplan <run>\plans\<plan>.json `
  --demo <demo.dem> `
  --out <run>\recording-<plan>-gameplay-120 `
  --hud gameplay `
  --fps 120 `
  --video-crf 16 `
  --timeout 45m
```

CS2 must launch through HLAE in windowed mode. The recorder should pass `-windowed`; verify the real `cs2.exe` command line if capture behavior changes.

5. Render with the standard preset:

```powershell
.\bin\zv.exe workflows run shorts-render -- `
  --recording-result <run>\recording-<plan>-gameplay-120\recording-result.json `
  --killplan <run>\plans\<plan>.json `
  --out <run>\renders\<plan>-viral-60-clean `
  --publish-dir <run>\shortslistosparasubir\<plan> `
  --preset viral-60-clean `
  --video-crf 16 `
  --video-preset slow `
  --hq-filters `
  --audio-normalize `
  --quality-checks `
  --cover-sheets
```

6. Assemble the reel:

- Put the final MP4, manifest, QA sheets, and any upload-ready assets under `<run>\shortslistosparasubir\<final-folder>`.
- Use concat only, not `xfade`.
- Reencode once to normalize timestamps, 60fps, H.264, AAC, and 1080x1920.
- Suggested labels:
  - Killer block: `N/TOTAL  LO MATAMOS - POV <KILLER>`
  - Target block: `N/TOTAL  SU POV - <TARGET> MUERE`

## QA Gate

Before delivery, generate contact strips from the actual final segment files, not only from source recordings.

For each death, sample at least three frames from the killer block and three from the target block. Inspect all deaths manually:

- Top/killer row shows the killer's HUD/player name and the kill action or setup.
- Bottom/target row shows the suspected cheater's HUD/player name and their death context.
- The sequence alternates correctly for every death: killer, target, killer, target.
- Every expected death is present exactly once and in tick order.
- Utility deaths are acceptable when the killer POV shows the killer's utility/setup rather than a direct shot.

If any killer block shows the target POV, recapture that killer using exact `name_in_demo` and rebuild the reel. Do not deliver until every block passes.

Run final probes:

```powershell
ffprobe -v error -show_entries stream=index,codec_type,codec_name,width,height,avg_frame_rate,duration -show_entries format=duration,size -of json <final.mp4>
```

Expected final: H.264 video, AAC audio, `1080x1920`, `60/1`, nonzero duration, no missing audio. Also confirm no HLAE/CS2/FragForge recording processes remain running for the run.

## Publishing Tone

Keep it funny and clip-focused. Use "suspected cheater" when the accusation is not proven, unless the user explicitly provides the final title wording. Do not encourage harassment or expose personal information; only use public game/Steam/channel identifiers when needed for context.
