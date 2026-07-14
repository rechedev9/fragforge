# Design: Fix Stream Clip Killfeed Overlay Crop

## Technical Approach

Backend-only fix in `internal/streamclips` plus one worker timestamp. Four independent corrections, each mapped to a delta-spec requirement: (1) bound `noticeSearchRegion` to the hint rect + fixed slop; (2) reject paired candidate boxes failing notice-shape filters mirrored from `internal/editor/killfeed_probe.go` (no import — constants duplicated with a comment pointing at the source); (3) sample cue frames at `cue + 0.35s` in the media worker while the overlay time gate keeps keying off the original cue; (4) tighten box growth from up to 3px to at most 1px beyond highlight pixels. Strict TDD via `make test`.

## Architecture Decisions

| # | Decision | Choice | Alternatives / Tradeoffs |
|---|---|---|---|
| a | Search-region bounding | In `noticeSearchRegion`, compute `maxX = bounds.Min.X + ceil((hint.X+hint.Width)*Dx)` and pad all four edges by existing `killfeedSearchPadding` (8px), then `Intersect(bounds)`. Nil-hint default region unchanged. | Percentage slop rejected: pixel slop matches existing Y padding and is predictable across resolutions. 8px keeps off-by-a-few hints working; more re-admits the HUD band. |
| b | Shape filters | New consts in `killfeed_detect.go`: `killfeedMinRowAspect = 2`, `killfeedMaxRowFill = 0.5` (mirror `killfeedMinHighlightAspect`, `killfeedMaxHighlightFill`). Height cap `frameHeight/12` already exists in `pairNoticeStrokes` — no new constant. Filter runs in `detectNoticeBoxes` after `pairNoticeStrokes`, before dedupe, so a HUD box can never swallow a real notice during IoU dedupe. Fill ratio counts **strict**-red pixels (150/55) inside the candidate box. | Fill on the loose mask (120/70) rejected: real notice interiors are loose-red (test fixtures use 130/45/45), so loose fill ≈ 1.0 would reject every real notice. Strict fill matches the editor probe, which counts strict components only. `killfeedMinHighlightPixels` deliberately NOT mirrored: stroke pairing already requires two ≥40px dense strokes, and a min-pixel floor would reject loose-only compressed rings (0 strict pixels). Importing `internal/editor` rejected per proposal (package boundary). |
| c | 0.35s delay location | Exported `KillfeedSampleDelaySeconds = 0.35` in `internal/streamclips/ffmpeg.go` beside `killfeedLeadTime`, with a comment stating `killfeedLeadTime` must stay ≥ the delay. `media_worker.go` adds it to the extraction `-ss` only. `buildKillfeedFilterGraph` is untouched: gate stays `cue−0.35 .. cue+2.8` off the original cue. Warnings keep printing the original cue. No EOF clamp: `-ss` past end fails frame decode and falls into the existing warn-and-omit path. | Baking delay into `DetectNoticeRows` rejected: detection is a pure image function; time belongs to the sampling caller (same split the editor uses via `killfeedSamplePart`). Worker-private const rejected: the invariant couples it to `killfeedLeadTime`, so both live in `streamclips`. |
| d | Bounds tightening | `killfeedLooseEdgeReach` 2 → 1; keep `killfeedRowMargin = 1`. Growth beyond captured highlight pixels is now the 1px margin only. | Removing `looseRedBounds` growth entirely rejected: it recovers anti-aliased ring edges that stroke means miss. Dropping the margin rejected: editor probe keeps a 1px margin (`killfeedHighlightMargin`) for the same anti-aliasing reason. |

## Data Flow

    media_worker (per cue)                      streamclips
    ────────────────────────                    ───────────
    ffmpeg -ss cue+0.35 ──► PNG ──► decode ──► DetectNoticeRows(frame, hint)
                                                 │ region = hint ± 8px (clamped)
                                                 │ pair strokes ─► shape filter ─► dedupe
                                                 │ grow ≤1px reach + 1px margin
                                                 ▼
    BuildFFmpegArgs(rows) ── overlay gate: between(cue−0.35, cue+2.8)  ◄ original cue

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/streamclips/killfeed_detect.go` | Modify | Bounded `noticeSearchRegion`; shape-filter step + consts; `killfeedLooseEdgeReach` 2→1 |
| `internal/streamclips/ffmpeg.go` | Modify | Add exported `KillfeedSampleDelaySeconds` const only; filtergraph unchanged |
| `internal/workers/media_worker.go` | Modify | Extraction `-ss` = `cue + streamclips.KillfeedSampleDelaySeconds` |
| `internal/streamclips/killfeed_detect_test.go` | Modify | New regression tests; repurpose right-edge test |
| `internal/workers/stream_worker_test.go` | Modify | `-ss` expectations become `0.850` / `1.600` |

## Interfaces / Contracts

`DetectNoticeRows(frame image.Image, hint *CropRect) []NoticeRow` signature unchanged. New exported const:

```go
// KillfeedSampleDelaySeconds delays cue-frame sampling so the notice
// highlight ring is fully drawn (mirrors the demo-editor probe).
// killfeedLeadTime must stay >= this delay so the overlay covers the cue.
const KillfeedSampleDelaySeconds = 0.35
```

## Testing Strategy

| Layer | What to Test | Approach (RED first, existing synthetic helpers reused) |
|-------|-------------|----------|
| Unit (`killfeed_detect_test.go`) | Region bounded to hint+slop | Notice inside hint detected; identical ring outside hint+8px (e.g. HUD band above / left of hint) returns no row. Repurpose `TestDetectNoticeRowsExpandsHintToFrameRightEdge` → bounded-region test (same hint/notice still passes: region maxX 1909 covers ring at 1621–1909). Reuses `drawStreamKillfeedNotice`. |
| Unit | Shape filters | Near-square solid red block (avatar) rejected by aspect; wide solid saturated bar (score/timer) rejected by strict fill; real, compressed, and mixed-style notices still accepted (existing three staggered/compressed tests must stay green). Reuses `fillStreamSolidRed`. |
| Unit | Tight bounds | New 1px-tolerance assertion helper against known synthetic extents (existing `assertNoticeRowNear` keeps 3px for legacy cases). |
| Integration (`stream_worker_test.go`) | `-ss` offset | Update expected values to `0.850`/`1.600`; failure-path warning strings unchanged (they print the original cue). |
| E2E (`stream_e2e_test.go`) | Staggered notices | No assertion changes expected: hint `{0.74,0.04,0.25,0.15}` on 720p yields region x≈[939,1276] y≈[20,145] covering both rings; drawboxes are static so sampling 2.35s equals 2.0s; overlay gate unchanged; region-mean assertions absorb ≤1px box drift. Run to verify. |

## Threat Matrix

N/A — all matrix rows (documentation paths, git selection, commit/push state, PR commands) have no boundary in this change. The only subprocess touch is a numeric `strconv.FormatFloat` timestamp in an existing FFmpeg invocation; no user-controlled string reaches a command line.

## Migration / Rollout

No migration required. Pure code change; revert restores prior behavior on next render.

## Open Questions

None.
