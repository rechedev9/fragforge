# HLAE Prototype — Findings

> Generated from a partial run on 2026-05-14. Only E3 was attempted; it
> exposed a root-cause blocker that invalidates the current carrier
> format for all four experiments. See **Summary** at the bottom and
> follow-up actions in the spec.

**Run date:** 2026-05-14
**HLAE version:** 2.190.0 (commit `038e10ab`, released 2026-05-06; includes AfxHookSource2 0.39.0)
**CS2 build:** `cs2.exe` last-modified 2026-04-29 18:30 UTC (VersionInfo strings empty in the binary, no console capture available — see Notes)
**Hardware:** AMD Ryzen 7 9800X3D 8-Core, AMD Radeon Graphics (iGPU), 31 GB RAM, Windows 11 26200.8037

---

## Top-line finding (applies to E1–E4)

**The `.mirv` carrier format used by `scripts/hlae/e[1-4]-*.mirv` is incompatible with HLAE 2.x.**

`mirv_script_load` in HLAE 2.190.0 runs a **JavaScript** engine (Boa JS), not a sequential `mirv_cmd add tick N "..."` text interpreter. When the runner does
`+mirv_script_load "scripts/hlae/e3-output-format.mirv"`, the JS parser rejects the file silently and **none of the scheduled commands fire**. The demo plays back normally and nothing is recorded.

Evidence:

- All snippets shipped with the install (`C:\HLAE\resources\AfxHookSource2\snippets\*.js`) are JavaScript and use the new event API:

  ```js
  "use strict";
  {
      const id = '...';
      mirv.events.clientFrameStageNotify.on(id, () => {
          if (mirv.getDemoTick() === 31746) {
              mirv.exec('mirv_streams record start');
          }
      });
  }
  ```

- `AfxHookSource2_changelog.xml` for 2.190.0 includes: *"mirv-script: reworked hooks to be `mirv.events.xxxxx`"* and adds `mirv.getDemoTick`, `mirv.getDemoTime`, `mirv.getCurTime`.
- After 13 minutes of CS2 wall-clock with the .mirv supposedly active, **zero files >10 KB** were written under `C:\Program Files (x86)\Steam\...\Counter-Strike Global Offensive\`, `C:\HLAE\`, or `%TEMP%\zv-hlae\`. No `e3-out.*` anywhere.

The spec's `errores y casos borde` table already listed this exact risk:
> Sintaxis `mirv_cmd add tick` no es la real → Actualizar este spec y los `.mirv` antes de seguir.

That mitigation now needs to execute.

---

## E1 — Seek accuracy

**Verdict:** ❌ not run (blocked by carrier finding above)

---

## E2 — Multi-segment in one CS2 session

**Verdict:** ❌ not run (blocked by carrier finding above)

---

## E3 — Output format

**Verdict:** ❌ fail (blocked at carrier load — `mirv_streams add` config never evaluated)

**Config that worked:** ☐ C1 (FFmpeg direct) · ☐ C2 (TGA raw) · **☒ neither — not evaluated**

**Command run:**

```powershell
.\run-experiment.ps1 -Experiment e3 `
    -Demo  "C:\Users\reche\Documents\zackvideo\testdata\lavked-vs-tnc-m2-nuke.dem" `
    -HlaeExe "C:\HLAE\HLAE.exe"
```

**Runner argv that reached HLAE** (after the two runner fixes below):

```
-customLoader -noGui -autoStart
-hookDllPath "C:\HLAE\x64\AfxHookSource2.dll"
-programPath "C:\Program Files (x86)\Steam\steamapps\common\Counter-Strike Global Offensive\game\bin\win64\cs2.exe"
-cmdLine "-insecure +playdemo \"...\\lavked-vs-tnc-m2-nuke.dem\" +mirv_script_load \"...\\e3-output-format.mirv\""
```

**Observations:**

| Stage | Result |
|---|---|
| HLAE launcher exits | 2.11 s wall-clock, exit code 0 (HLAE detaches after dispatch) |
| CS2 process | started, ran 13 min 13 s, peak 3.5 GB RAM, 1220 s CPU |
| Demo visible on screen | yes — user-confirmed |
| `e3-out.*` produced anywhere on disk | no |
| Any file >10 KB written under CS2 install / HLAE / TEMP\zv-hlae in the window | no |
| `quit` (scheduled at tick 32500) ever fired | no — CS2 had to be killed manually |

**Why E3 stalled:** the `.mirv` was loaded by `mirv_script_load`, which expects JavaScript. Lines like `mirv_cmd add tick 25 "..."` are not valid JS and were rejected; no schedule was installed, so `demo_gototick`, `mirv_streams add`, `mirv_streams record start/end`, `disconnect`, and `quit` never executed. The demo just played from tick 0 at 1× until killed.

**Downstream:** the C1 vs C2 question (FFmpeg pipe vs TGA frames) is **still open** — we never reached `mirv_streams add ffmpeg`.

---

## E4 — `host_timescale 2` during recording

**Verdict:** ❌ not run (blocked by carrier finding above)

---

## Runner fixes applied during the session

The runner (`scripts/hlae/run-experiment.ps1`) had three problems that surfaced before the carrier issue was visible. All three are committed (or staged in the same commit as this findings file):

1. **Hook DLL location.** The HLAE 2.x portable ZIP puts `AfxHookSource2.dll` in `x64\`, not next to `HLAE.exe`. Runner's existence check now accepts either layout. (commit `a6e6d06`)
2. **Launcher flag.** `-csgoLauncher` is CS:GO-specific and fails on `cs2.exe` with HLAE error 2002 / Win32Error 123 (`ERROR_INVALID_NAME`). Replaced with `-customLoader` per the HLAE wiki's "Custom Loader" recipe.
3. **`-insecure` required.** AfxHookSource2 refuses to attach without `-insecure` in the launch options ("Please add -insecure to launch options, AfxHookSource2 will refuse to work without it!"). Prepended to the CS2 cmdline inside `-cmdLine`.

After all three, HLAE injected cleanly and CS2 played the demo — but the carrier-format issue is downstream of injection.

---

## Summary for the Spec 2 (`zv-recorder`) handoff

| Question | Answer |
|---|---|
| Real `pre_roll` needed (seconds) | **Unknown** — not measured, E1 blocked. |
| One CS2 session per demo, or per segment? | **Unknown** — E2 blocked. |
| `mirv_streams` config to use (C1 vs C2) | **Unknown** — E3 blocked before reaching `mirv_streams add`. |
| Default `host_timescale` | **Unknown** — E4 blocked. |
| New restriction to add | **Carrier must be `.js` (mirv-script Boa JS), not `.mirv` text commands.** The `mirv_script_load` API in HLAE 2.x runs JavaScript via `mirv.events.*` + `mirv.exec(...)`. |

### Recommended follow-ups (before any `zv-recorder` Go code)

1. **Rewrite `scripts/hlae/e[1-4]-*.mirv` as `e[1-4]-*.js`** using the supported API:
   - schedule with `mirv.events.clientFrameStageNotify.on(id, () => { if (mirv.getDemoTick() >= target) { ... } })`
   - issue engine commands with `mirv.exec('mirv_streams add ffmpeg main "..."')`, `mirv.exec('demo_gototick N')`, etc.
   - guard one-shot triggers (the frame callback fires every frame; track a "done" flag per scheduled command).
2. **Update the runner** to pass `+mirv_script_load <path.js>` (no path change needed; the file extension is conventional, not enforced — but rename for clarity).
3. **Decide whether the runner should wait on CS2 instead of HLAE.** `WaitForExit()` currently returns after HLAE detaches (~2 s), so the runner can't report whether the recording finished. Either poll the CS2 process or have the script `mirv.exec('quit')` and have the runner read a sentinel file written from JS.
4. **Update `docs/specs/2026-05-14-hlae-prototype.md`**: replace the "Estructura de cada `.mirv`" section with the JS shape and update the conceptual example.
5. Re-run E3 first (as originally planned) — it remains the right gate because the C1 vs C2 question is still open.

### What is already verified

- HLAE 2.190.0 injects into `cs2.exe` and the hook loads (the AfxHookSource2 init dialog asking for `-insecure` is the proof; after adding `-insecure`, CS2 plays the demo with the hook attached).
- The runner's process orchestration, path validation, and arg quoting all work end-to-end.
- CS2 install path, demo path, and ffprobe install (`Gyan.FFmpeg 8.1.1` via winget) are all in place.

So infrastructure is green; only the scripting layer needs the rewrite.

---

## Notes

- CS2 doesn't expose a useful FileVersion on `cs2.exe` (the metadata strings are empty), so the exact CS2 build couldn't be captured automatically. To capture next time: launch CS2 with `-condebug`, read `console.log` for the `version` line. Or run `version` in the console manually.
- The watcher used to wait for CS2 to exit (`scripts/hlae/` is not the right place for this — it lived inline in the operator's session) timed out at 10 min and would print a misleading "CS2 exited" message even on timeout. If the rewrite goes the "runner waits on CS2" route (recommendation #3 above), that watcher logic should live in the runner with a correct exit-vs-timeout branch.
- No `.mp4` sample produced; the "sample link" requirement in the spec's closing criteria is deferred until after the rewrite.
