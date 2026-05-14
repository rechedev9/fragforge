# HLAE Prototype Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce the artifacts that the user runs on their Windows PC to validate the 4 open HLAE questions: a PowerShell runner, four `.mirv` scripts (one per experiment), a README with step-by-step instructions, and a findings template.

**Architecture:** No Go code is written in this slice. The deliverables are pure text artifacts. The user runs them on Windows; correctness is judged by what the recorded `.mp4` looks like and what `ffprobe` reports.

**Tech Stack:** PowerShell (the runner), HLAE `.mirv` script syntax (the experiments), Markdown (README + findings template).

**Spec reference:** [`2026-05-14-hlae-prototype.md`](./2026-05-14-hlae-prototype.md).

---

## Slice scope

**In:**
- `scripts/hlae/run-experiment.ps1` — single PowerShell runner that dispatches by experiment name.
- `scripts/hlae/e1-seek-accuracy.mirv` — seek-precision test against `seg-001` of `lavked-vs-tnc-m2-nuke.dem`.
- `scripts/hlae/e2-multi-segment.mirv` — 3 segments recorded in one CS2 session.
- `scripts/hlae/e3-output-format.mirv` — output-format probe with two candidate `mirv_streams` configs.
- `scripts/hlae/e4-host-timescale.mirv` — E1 repeated with `host_timescale 2`.
- `scripts/hlae/README.md` — step-by-step instructions the user follows on Windows.
- `docs/research/07-hlae-prototype-results.md.template` — empty findings doc with one table per experiment.

**Out:**
- Any Go code (`zv-recorder` is a separate slice).
- Running the experiments — the user does that on their PC.
- Choosing a final `mirv_streams` config — that comes out of the experiments themselves.

---

## File structure

```
zackvideo/
├── scripts/
│   └── hlae/
│       ├── README.md
│       ├── run-experiment.ps1
│       ├── e1-seek-accuracy.mirv
│       ├── e2-multi-segment.mirv
│       ├── e3-output-format.mirv
│       └── e4-host-timescale.mirv
└── docs/
    └── research/
        └── 07-hlae-prototype-results.md.template
```

Each file has one responsibility:

- `run-experiment.ps1` — invokes HLAE with the right `.mirv` script and parameters. Knows nothing about the experiments themselves.
- `eN-*.mirv` — declarative description of one experiment. No logic.
- `README.md` — operator runbook.
- `07-hlae-prototype-results.md.template` — what the user fills in.

---

## Task 1: Create the `scripts/hlae/` directory and PowerShell runner

**Files:**
- Create: `scripts/hlae/run-experiment.ps1`

- [ ] **Step 1: Create the directory**

Run: `mkdir -p scripts/hlae`
Expected: directory exists.

- [ ] **Step 2: Write `scripts/hlae/run-experiment.ps1`**

```powershell
<#
.SYNOPSIS
    Runs a single HLAE prototype experiment by name.

.DESCRIPTION
    Launches CS2 through HLAE with the experiment's .mirv script preloaded,
    waits for the game to exit, and reports the output directory.

.PARAMETER Experiment
    Experiment id (one of e1, e2, e3, e4).

.PARAMETER Demo
    Absolute path to the .dem file (expected: lavked-vs-tnc-m2-nuke.dem).

.PARAMETER HlaeExe
    Absolute path to HLAE.exe.

.PARAMETER Cs2Exe
    Absolute path to cs2.exe. Defaults to the Steam install path.

.PARAMETER OutDir
    Where HLAE writes recordings and frames.
    Defaults to "$env:TEMP\zv-hlae\<experiment>".

.EXAMPLE
    .\run-experiment.ps1 -Experiment e1 `
        -Demo "C:\demos\lavked-vs-tnc-m2-nuke.dem" `
        -HlaeExe "C:\HLAE\HLAE.exe"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('e1', 'e2', 'e3', 'e4')]
    [string]$Experiment,

    [Parameter(Mandatory = $true)]
    [string]$Demo,

    [Parameter(Mandatory = $true)]
    [string]$HlaeExe,

    [string]$Cs2Exe = "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe",

    [string]$OutDir
)

$ErrorActionPreference = 'Stop'

# Resolve script directory to find the .mirv file.
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$MirvMap = @{
    'e1' = 'e1-seek-accuracy.mirv'
    'e2' = 'e2-multi-segment.mirv'
    'e3' = 'e3-output-format.mirv'
    'e4' = 'e4-host-timescale.mirv'
}
$MirvPath = Join-Path $ScriptDir $MirvMap[$Experiment]

# Validate inputs.
if (-not (Test-Path $Demo))     { throw "Demo not found: $Demo" }
if (-not (Test-Path $HlaeExe))  { throw "HLAE not found: $HlaeExe" }
if (-not (Test-Path $Cs2Exe))   { throw "CS2 not found: $Cs2Exe" }
if (-not (Test-Path $MirvPath)) { throw "Mirv script not found: $MirvPath" }

if (-not $OutDir) {
    $OutDir = Join-Path $env:TEMP "zv-hlae\$Experiment"
}
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

Write-Host "Experiment : $Experiment"
Write-Host "Demo       : $Demo"
Write-Host "Mirv script: $MirvPath"
Write-Host "Output dir : $OutDir"
Write-Host ""

# Build HLAE arguments.
$HookDll = Join-Path (Split-Path -Parent $HlaeExe) 'AfxHookSource2.dll'
if (-not (Test-Path $HookDll)) {
    throw "AfxHookSource2.dll not found next to HLAE.exe at $HookDll"
}

$CmdLine = "+playdemo `"$Demo`" +mirv_script_load `"$MirvPath`""

$Args = @(
    '-csgoLauncher',
    '-noGui',
    '-autoStart',
    '-hookDllPath',  "`"$HookDll`"",
    '-programPath',  "`"$Cs2Exe`"",
    '-cmdLine',      "`"$CmdLine`""
)

# Track wall-clock time (needed for E4).
$sw = [System.Diagnostics.Stopwatch]::StartNew()
Write-Host "Launching HLAE..."
$proc = Start-Process -FilePath $HlaeExe -ArgumentList $Args -Wait -PassThru -NoNewWindow
$sw.Stop()

Write-Host ""
Write-Host "HLAE exited with code $($proc.ExitCode)"
Write-Host "Wall-clock duration: $([math]::Round($sw.Elapsed.TotalSeconds, 2)) s"
Write-Host ""
Write-Host "Output directory contents:"
Get-ChildItem -Path $OutDir -Recurse | Format-Table FullName, Length
```

- [ ] **Step 3: Verify the file**

Run: `head -5 scripts/hlae/run-experiment.ps1 && wc -l scripts/hlae/run-experiment.ps1`
Expected: shows the synopsis comment and around 100 lines.

- [ ] **Step 4: Commit**

```bash
git add scripts/hlae/run-experiment.ps1
git commit -m "feat(hlae): add PowerShell runner for prototype experiments"
```

---

## Task 2: Write `e1-seek-accuracy.mirv`

**Files:**
- Create: `scripts/hlae/e1-seek-accuracy.mirv`

**Context:** Seg-001 of `lavked-vs-tnc-m2-nuke.expected.json`. `tick_start = 22086`, first kill at `tick = 22278`, AccountID of target is `188721128` (derived from SteamID64 `76561198148986856`). Tickrate 64 → first kill is `22278 - 22086 = 192` ticks = 3.0 s into the recording.

- [ ] **Step 1: Write `scripts/hlae/e1-seek-accuracy.mirv`**

```text
// e1-seek-accuracy.mirv
// Goal: measure the real offset between demo_gototick T and the first
// recorded frame. The first kill of seg-001 is at tick 22278; with
// tick_start = 22086 it should appear ~3.0s into the .mp4.

// Schedule everything against tick numbers. Ticks before tick_start
// are "set-up" ticks; ticks within [tick_start, tick_end] drive the
// actual record window.

mirv_cmd add tick 50    "demo_gototick 22086"
mirv_cmd add tick 100   "spec_player_by_accountid 188721128"

// E3 confirms the real syntax; for E1 we want a single mp4 out.
mirv_cmd add tick 22086 "mirv_streams record start"
mirv_cmd add tick 22406 "mirv_streams record end"

mirv_cmd add tick 22500 "disconnect"
mirv_cmd add tick 22600 "quit"
```

- [ ] **Step 2: Verify**

Run: `grep -c 'mirv_cmd add tick' scripts/hlae/e1-seek-accuracy.mirv`
Expected: `6`

- [ ] **Step 3: Commit**

```bash
git add scripts/hlae/e1-seek-accuracy.mirv
git commit -m "feat(hlae): add seek-accuracy experiment (E1)"
```

---

## Task 3: Write `e2-multi-segment.mirv`

**Files:**
- Create: `scripts/hlae/e2-multi-segment.mirv`

**Context:** Three segments from the kill plan:
- seg-001: `22086 → 22868` (round 4, 3-kill spree).
- seg-002: `31746 → 32258` (round 5, 1 kill).
- seg-003: `34586 → 35098` (round 6, 1 kill).

The gaps between them are large enough that `demo_gototick` between segments has to actually seek (~36–138 s of demo time).

- [ ] **Step 1: Write `scripts/hlae/e2-multi-segment.mirv`**

```text
// e2-multi-segment.mirv
// Goal: verify N segments can be recorded inside a single CS2 session,
// each producing its own output file.

// Seg-001: tick_start=22086, tick_end=22868
mirv_cmd add tick 50    "demo_gototick 22086"
mirv_cmd add tick 100   "spec_player_by_accountid 188721128"
mirv_cmd add tick 22086 "mirv_streams record start"
mirv_cmd add tick 22868 "mirv_streams record end"

// Seg-002: tick_start=31746, tick_end=32258
mirv_cmd add tick 22900 "demo_gototick 31746"
mirv_cmd add tick 31700 "spec_player_by_accountid 188721128"
mirv_cmd add tick 31746 "mirv_streams record start"
mirv_cmd add tick 32258 "mirv_streams record end"

// Seg-003: tick_start=34586, tick_end=35098
mirv_cmd add tick 32300 "demo_gototick 34586"
mirv_cmd add tick 34540 "spec_player_by_accountid 188721128"
mirv_cmd add tick 34586 "mirv_streams record start"
mirv_cmd add tick 35098 "mirv_streams record end"

mirv_cmd add tick 35200 "disconnect"
mirv_cmd add tick 35300 "quit"
```

- [ ] **Step 2: Verify**

Run: `grep -c 'mirv_streams record start' scripts/hlae/e2-multi-segment.mirv && grep -c 'mirv_streams record end' scripts/hlae/e2-multi-segment.mirv`
Expected: `3` then `3`.

- [ ] **Step 3: Commit**

```bash
git add scripts/hlae/e2-multi-segment.mirv
git commit -m "feat(hlae): add multi-segment experiment (E2)"
```

---

## Task 4: Write `e3-output-format.mirv`

**Files:**
- Create: `scripts/hlae/e3-output-format.mirv`

**Context:** Probe the real output of `mirv_streams`. Try the H.264-via-FFmpeg config first (C1); if HLAE rejects the syntax, the user falls back to the raw-frames config (C2) by editing the file. Uses seg-002 (`31746 → 32258`) — simplest case, one kill.

- [ ] **Step 1: Write `scripts/hlae/e3-output-format.mirv`**

```text
// e3-output-format.mirv
// Goal: determine the real output format produced by mirv_streams.
//
// Two configs are tried in order. C1 first; if HLAE rejects it,
// comment out C1 and uncomment C2, then re-run the experiment.

// ---------- C1: H.264 directly via embedded FFmpeg ----------
mirv_cmd add tick 25    "mirv_streams add ffmpeg main \"-c:v libx264 -preset slow -crf 18 -pix_fmt yuv420p -r 60 -s 1920x1080 -y e3-out.mp4\""

// ---------- C2: raw TGA frames (uncomment if C1 fails) ----------
// mirv_cmd add tick 25  "mirv_streams add tga main"

mirv_cmd add tick 50    "demo_gototick 31746"
mirv_cmd add tick 100   "spec_player_by_accountid 188721128"
mirv_cmd add tick 31746 "mirv_streams record start"
mirv_cmd add tick 32258 "mirv_streams record end"

mirv_cmd add tick 32400 "disconnect"
mirv_cmd add tick 32500 "quit"
```

- [ ] **Step 2: Verify**

Run: `grep -E 'mirv_streams add (ffmpeg|tga)' scripts/hlae/e3-output-format.mirv`
Expected: shows the C1 line uncommented and the C2 line commented.

- [ ] **Step 3: Commit**

```bash
git add scripts/hlae/e3-output-format.mirv
git commit -m "feat(hlae): add output-format experiment (E3)"
```

---

## Task 5: Write `e4-host-timescale.mirv`

**Files:**
- Create: `scripts/hlae/e4-host-timescale.mirv`

**Context:** Same record window as E1 but with `host_timescale 2` active during the record window. Wall-clock should be ~50% of E1; the recorded `.mp4` should be identical in length and frame count.

- [ ] **Step 1: Write `scripts/hlae/e4-host-timescale.mirv`**

```text
// e4-host-timescale.mirv
// Goal: check whether host_timescale 2 produces a clean recording.
// Identical to e1 except for the host_timescale wrap around the
// record window.

mirv_cmd add tick 50    "demo_gototick 22086"
mirv_cmd add tick 100   "spec_player_by_accountid 188721128"

// Speed up just for the record window.
mirv_cmd add tick 22080 "host_timescale 2"
mirv_cmd add tick 22086 "mirv_streams record start"
mirv_cmd add tick 22406 "mirv_streams record end"
mirv_cmd add tick 22410 "host_timescale 1"

mirv_cmd add tick 22500 "disconnect"
mirv_cmd add tick 22600 "quit"
```

- [ ] **Step 2: Verify**

Run: `grep 'host_timescale' scripts/hlae/e4-host-timescale.mirv`
Expected: two lines, `host_timescale 2` then `host_timescale 1`.

- [ ] **Step 3: Commit**

```bash
git add scripts/hlae/e4-host-timescale.mirv
git commit -m "feat(hlae): add host_timescale experiment (E4)"
```

---

## Task 6: Write `scripts/hlae/README.md` (operator runbook)

**Files:**
- Create: `scripts/hlae/README.md`

- [ ] **Step 1: Write `scripts/hlae/README.md`**

````markdown
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
````

- [ ] **Step 2: Verify**

Run: `wc -l scripts/hlae/README.md && head -3 scripts/hlae/README.md`
Expected: around 80 lines, first line is `# HLAE Prototype — Operator Runbook`.

- [ ] **Step 3: Commit**

```bash
git add scripts/hlae/README.md
git commit -m "docs(hlae): add operator runbook for prototype experiments"
```

---

## Task 7: Write the findings template

**Files:**
- Create: `docs/research/07-hlae-prototype-results.md.template`

- [ ] **Step 1: Write `docs/research/07-hlae-prototype-results.md.template`**

````markdown
# HLAE Prototype — Findings

> **How to use this file:** copy to `07-hlae-prototype-results.md`
> (drop the `.template`), fill in each experiment as you run it on
> your Windows PC, then commit. See
> [`../specs/2026-05-14-hlae-prototype.md`](../specs/2026-05-14-hlae-prototype.md)
> for the spec.

**Run date:** _YYYY-MM-DD_
**HLAE version:** _e.g. 2.144.0_
**CS2 build:** _from `version` in the console, e.g. 1.40.7.0_
**Hardware:** _GPU, CPU, RAM_

---

## E1 — Seek accuracy

**Verdict:** ☐ ✅ pass · ☐ ❌ fail · ☐ ⚠️ partial

**Command run:**

```powershell
.\run-experiment.ps1 -Experiment e1 -Demo "..." -HlaeExe "..."
```

**`ffprobe` output:**

```
duration       =
nb_frames      =
codec_name     =
width x height =
r_frame_rate   =
```

**Visual inspection:**

| Question                                        | Answer |
|-------------------------------------------------|--------|
| Frame at which the first kill appears           |        |
| Expected frame (= `(22278-22086)/64 * fps`)     |        |
| Offset in frames                                |        |
| Offset in ticks (= offset_frames * 64 / fps)    |        |

**Notes / surprises:**

> _...write here..._

**Decision for `zv-recorder`:**

> _e.g. offset is ±1 tick → keep pre_roll = 3 s default._

---

## E2 — Multi-segment in one CS2 session

**Verdict:** ☐ ✅ pass · ☐ ❌ fail · ☐ ⚠️ partial

**Output directory contents:**

```
$env:TEMP\zv-hlae\e2\
    ...
```

| Segment | File present? | Playable? | Duration | Notes |
|---------|---------------|-----------|----------|-------|
| seg-001 |               |           |          |       |
| seg-002 |               |           |          |       |
| seg-003 |               |           |          |       |

**Decision for `zv-recorder`:**

> _e.g. 3 valid files → 1 CS2 session per demo. Or: only seg-001 valid → 1 CS2 per segment._

**Sample `.mp4` link (commit before merging this file):**

> _URL_

---

## E3 — Output format

**Verdict:** ☐ ✅ pass · ☐ ❌ fail · ☐ ⚠️ partial

**Config that worked:** ☐ C1 (FFmpeg direct) · ☐ C2 (TGA raw) · ☐ neither

**`ffprobe` output (or list of TGAs):**

```
...
```

| Property               | Value |
|------------------------|-------|
| Codec                  |       |
| Container              |       |
| Resolution             |       |
| FPS                    |       |
| Bitrate                |       |
| File size (MB)         |       |
| Audio track present?   |       |
| Audio sounds correct?  |       |

**Decision for `zv-recorder`:**

> _e.g. C1 works → consume `.mp4` directly. Or: only C2 works → recorder pipes TGAs through FFmpeg after recording._

---

## E4 — `host_timescale 2` during recording

**Verdict:** ☐ ✅ pass · ☐ ❌ fail · ☐ ⚠️ partial

| Metric                       | E1 baseline | E4 (timescale 2) |
|------------------------------|-------------|-------------------|
| Wall-clock (s)               |             |                   |
| Video duration (s)           |             |                   |
| Frame count                  |             |                   |

**Artifacts observed:**

| Phenomenon       | Present? | Notes |
|------------------|----------|-------|
| Frame drops      |          |       |
| Frame repeats    |          |       |
| Tearing          |          |       |
| Audio glitches   |          |       |
| Audio desync     |          |       |

**Decision for `zv-recorder`:**

> _e.g. clean and ~50% wall-clock → default to `host_timescale 2`. Or: glitches → stay at 1×._

---

## Summary for the Spec 2 (`zv-recorder`) handoff

| Question                                            | Answer (1-2 lines) |
|-----------------------------------------------------|--------------------|
| Real `pre_roll` needed (seconds)                    |                    |
| One CS2 session per demo, or per segment?           |                    |
| `mirv_streams` config to use (C1 vs C2)             |                    |
| Default `host_timescale`                            |                    |
| Any new restriction to add to `zv-recorder` spec    |                    |
````

- [ ] **Step 2: Verify**

Run: `grep -c '^## E' docs/research/07-hlae-prototype-results.md.template`
Expected: `4`

- [ ] **Step 3: Commit**

```bash
git add docs/research/07-hlae-prototype-results.md.template
git commit -m "docs(hlae): add findings template for prototype experiments"
```

---

## Task 8: Update `docs/README.md` so the prototype is discoverable

**Files:**
- Modify: `docs/README.md`

- [ ] **Step 1: Read the current "Specs activos" section**

Run: `grep -A 5 'Specs activos' docs/README.md`
Expected: shows the existing demo-parser-slice line.

- [ ] **Step 2: Replace the "Specs activos" block**

Find:

```markdown
Specs activos:
- [`specs/2026-05-14-demo-parser-slice.md`](./specs/2026-05-14-demo-parser-slice.md) — `zv-parser`, primer binario implementable. Esperando aprobación.
```

Replace with:

```markdown
Specs activos:
- [`specs/2026-05-14-demo-parser-slice.md`](./specs/2026-05-14-demo-parser-slice.md) — `zv-parser` (implementado).
- [`specs/2026-05-14-orchestrator-slice-plan.md`](./specs/2026-05-14-orchestrator-slice-plan.md) — `zv-orchestrator` (implementado).
- [`specs/2026-05-14-hlae-prototype.md`](./specs/2026-05-14-hlae-prototype.md) — HLAE prototype sub-slice (pendiente de ejecutar en Windows).
- [`specs/2026-05-14-hlae-prototype-plan.md`](./specs/2026-05-14-hlae-prototype-plan.md) — plan ejecutable del prototipo HLAE.
```

- [ ] **Step 3: Verify**

Run: `grep -A 6 'Specs activos' docs/README.md`
Expected: four bullet items as above.

- [ ] **Step 4: Commit**

```bash
git add docs/README.md
git commit -m "docs: list orchestrator + HLAE prototype specs in README"
```

---

## Self-review

**Spec coverage (against `2026-05-14-hlae-prototype.md`):**

- Spec §2 (pre-requisites) — covered in `scripts/hlae/README.md`. Task 6 ✓
- Spec §3.E1 (seek accuracy) — `e1-seek-accuracy.mirv`. Task 2 ✓
- Spec §3.E2 (multi-segment) — `e2-multi-segment.mirv`. Task 3 ✓
- Spec §3.E3 (output format, C1 + C2) — `e3-output-format.mirv`. Task 4 ✓
- Spec §3.E4 (host_timescale) — `e4-host-timescale.mirv`. Task 5 ✓
- Spec §4 (artifacts to deliver — runner) — `run-experiment.ps1`. Task 1 ✓
- Spec §4 (findings template) — Task 7 ✓
- Spec §5 (per-experiment protocol) — runbook + template. Tasks 6, 7 ✓
- Spec §6 (acceptance criteria) — captured as the summary table in the template. Task 7 ✓
- Discoverability of the spec — Task 8 ✓

**Placeholder scan:** none — every `.mirv` and PowerShell snippet is shown in full.

**Type / parameter consistency:**

- AccountID `188721128` appears identically in E1, E2, E3, E4.
- The four experiment ids in `run-experiment.ps1`'s `ValidateSet` (`e1`–`e4`) match the file names in `scripts/hlae/`.
- The `$OutDir` default `"$env:TEMP\zv-hlae\<experiment>"` matches what the README tells the user to inspect.
- Tick values come from `testdata/lavked-vs-tnc-m2-nuke.expected.json` and are consistent across experiments.

**Independent execution windows for subagents:** Task 1 (runner) has no dependencies on the `.mirv` files (it dispatches by name). Tasks 2–5 (the four `.mirv` files) are completely independent and can run in parallel. Task 6 (README) references file names from Tasks 1–5 but does not depend on their content. Task 7 (template) is independent. Task 8 (docs/README update) depends on this plan file being committed but on no other task. Order to maximize parallelism: 1 → {2, 3, 4, 5, 7} in parallel → 6 → 8.
