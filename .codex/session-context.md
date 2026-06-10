# FragForge Session Context

Last updated: 2026-05-29

## Current Direction

The local UI experiment was removed at the user's request. Do not reintroduce UI
or Playwright work unless explicitly asked.

The current video direction is deterministic music-synced CS2 Shorts/long-form
compilations, inspired by a WhatsApp reference clip where killfeed/kill moments
land shortly after musical beats.

Reference clip used in this session:

```powershell
c:\Users\reche\Downloads\WhatsApp Video 2026-05-29 at 16.19.58.mp4
```

Detected reference rhythm:

- BPM: 103.36
- Beat period: 0.58s
- Beat phase: 0.575s
- Target kill impact offset: beat + 100ms

Generated analysis artifact:

```powershell
data\analysis\whatsapp-reference\zv-rhythm.json
```

## Implemented In This Session

Added a deterministic rhythm-analysis stage:

- `internal/rhythm`: FFmpeg audio decode, RMS onset envelope, BPM estimate,
  beat grid, strong onsets, and optional killplan-to-beat `segment_sync`.
- `cmd/zv-rhythm`: `zv-rhythm analyze --input <audio-or-video> --out <rhythm.json>`.
- Canonical CLI integration:
  `zv music analyze --input <audio-or-video> --out <rhythm.json> [--killplan <plan.json>]`.
- Workflow catalog integration:
  `zv workflows run music-analyze -- --input <audio-or-video> --out <rhythm.json>`.
- Build integration in `Makefile` and `scripts/build.ps1`.

Validation passed:

```powershell
bash scripts/go-gate.sh --no-format
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
.\bin\zv.exe music analyze --input "c:\Users\reche\Downloads\WhatsApp Video 2026-05-29 at 16.19.58.mp4" --out data\analysis\whatsapp-reference\zv-rhythm.json
```

## Next Step

Make the long/Shorts renderer consume `segment_sync` from the rhythm JSON so
segment starts, gaps, and cuts are placed on the music grid. The current work
only produces the rhythm/sync plan; it does not yet alter final composition.

Recommended next implementation path:

1. Add a rhythm JSON input flag to the relevant composition/editor command.
2. Map `segment_sync.timeline_start_seconds` into the concat/composition plan.
3. Preserve the default current behavior when no rhythm JSON is provided.
4. Add unit tests around timeline placement without invoking CS2/HLAE or long
   FFmpeg renders.

## Constraints To Preserve

- Do not run HLAE/CS2 or long renders unless explicitly asked.
- Use `C:\HLAE-2.190.1\HLAE.exe` for capture if capture is later requested.
- Treat `data/` media outputs as generated artifacts.
- Avoid Redis/UI rework unless the user explicitly reopens those topics.
