---
name: zackvideo-cs2-utility-shorts
description: "Create CS2 utility Shorts from a demo with FragForge: parse utility, audit destinations/actions, record, render with the standard viral-60-clean preset, and open the gallery."
---

# FragForge CS2 Utility Shorts

Use this skill when the user wants Shorts about CS2 utility from a demo, especially smokes/flashes/molotovs for one player. Use the standard `viral-60-clean` preset; alternate effects presets are retired.

## Creative Brief Gate

Before any non-dry-run capture or render, ask the user only for the creative choices they have not already supplied, grouped into one concise message, and wait for explicit approval:

- delivery format: `short-9x16` (default) or `landscape-16x9`;
- HUD and killfeed treatment: `gameplay` (full HUD), `deathnotices`, or `clean`;
- which utility to include: all audited lineups, one map, or a named selection;
- destination/stance labels: keep the audited labels in captions/metadata or a custom wording;
- kill effect and transition when kills appear in the clips: `clean`, `punch-in`, `velocity`, or `freeze-flash`, and `cut`, `flash`, `whip`, or `dip`;
- kill numbering/counter when kills appear in the clips: disabled or enabled with the built-in milestone labels;
- intro/outro text and music;
- thumbnail strategy: generated gameplay cover candidates or no cover.

If the user delegates creative control, state the resolved defaults and treat that delegation as approval.
Preserve every approved answer in the exact recording and render argv; do not silently replace them with preset defaults later.
After cover candidates exist, show them and ask the user to choose the final thumbnail before calling the pack upload-ready, unless the user delegated that choice.

## Workflow

1. Parse utility segments:

```powershell
.\bin\zv.exe workflows run demo-parse -- `
  --demo <demo.dem> `
  --steamid <SteamID64> `
  --segment-mode utility `
  --out <run>\plan-utility.json `
  --verbose
```

2. Audit utility metadata before trusting labels:

```powershell
.\bin\zv.exe workflows run utility-audit -- `
  --plan <run>\plan-utility.json `
  --lineup-catalog data\lineups `
  --out <run>\utility-audit.csv
```

Check `destination_source`. Treat `catalog` as reviewed and `auto` as a guess that may need manual review.

3. Record the plan:

```powershell
.\bin\zv.exe workflows run record -- `
  --killplan <run>\plan-utility.json `
  --demo <demo.dem> `
  --out <run>\recording `
  --hlae <HLAE.exe> `
  --cs2 <cs2.exe>
```

Use `--dry-run` first when changing recording settings.

4. Render Shorts:

```powershell
.\bin\zv.exe workflows run shorts-render -- `
  --recording-result <run>\recording\recording-result.json `
	--killplan <run>\plan-utility.json `
	--out <run>\shorts-utility `
	--publish-dir <run>\shortslistosparasubir `
	--preset viral-60-clean `
  --lineup-catalog data\lineups
```

Use `--skip-existing` only when changing captions/metadata but not burned-in overlay text.

5. Open the review gallery:

```powershell
.\bin\zv.exe workflows run gallery-open -- --path <run>\shortslistosparasubir\index.html
```

## Review Rules

- Confirm the selected utility moment is visible in the clean POV.
- Keep destination/action labels in metadata and captions unless a custom Lua script is explicitly requested.
- Include stance and action when known in captions or review notes: `STANDING JUMPTHROW`, `CROUCH JUMPTHROW`, `RUNNING THROW`, `WALKING THROW`.

## Destination Rules

- Prefer manual entries in `data/lineups/*.smokes.json` over auto inference.
- If a landing destination is wrong, add or adjust a catalog entry and rerender without `--skip-existing`.
- Known Inferno correction: iM `seg-001` is a CT Spawn to T Ramp smoke and should display `T RAMP SMOKE` with `STANDING JUMPTHROW`.
