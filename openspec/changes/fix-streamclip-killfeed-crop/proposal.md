# Proposal: Fix Stream Clip Killfeed Overlay Crop

## Problem Statement

In the stream clip → vertical Short flow, the "clean killfeed" overlay strip includes extra HUD content (score/timer row, player avatar row) and misaligned/blurry notices instead of tight per-notice crops. Root causes (verified): (1) `noticeSearchRegion` extends the user hint to the frame's right edge and pads Y, overlapping the CS2 top-HUD band whose red elements pass the loose thresholds — false rows stack above real notices; (2) cue frames are sampled at the exact user-marked second with no fade-in delay (demo path uses 0.35s); (3) box bounds grow up to 3px beyond the notice.

## Intent

Make the stream-clip killfeed overlay pixel-accurate: only real kill notices, tightly cropped, sampled when the highlight ring is fully visible.

## Scope

### In Scope
- Constrain detection search region to the user hint rect with small bounded slop (no extension to frame right edge).
- Anti-HUD false-positive shape filters (aspect ratio, max fill, max height), mirroring `internal/editor/killfeed_probe.go` constants.
- Add 0.35s sample delay to cue frame extraction in the media worker stream path.
- Tighten detected box bounds where feasible.
- Regression tests for all of the above.

### Out of Scope
- Web preview alignment / preview-render drift (`web/components/streams/stream-preview.tsx`) — separate follow-up.
- Demo pipeline killfeed (`internal/editor`).
- Layout variants or new presets.

## Capabilities

### New Capabilities
- `streamclip-killfeed-overlay`: detection, cropping, and cue sampling behavior of the stream-clip clean killfeed overlay.

### Modified Capabilities
- None (no existing specs in `openspec/specs/`).

## Approach

Backend-only fix in `internal/streamclips` plus one worker constant. Bound `noticeSearchRegion` to the hint rect ± small slop; reject candidate rows failing shape filters analogous to the demo-editor probe; extract cue PNGs at `cue + 0.35s`; reduce bounds growth/margin. Strict TDD (`make test`) with table-driven regression tests using synthetic frames.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/streamclips/killfeed_detect.go` | Modified | Bounded search region, shape filters, tighter bounds |
| `internal/streamclips/ffmpeg.go` | Modified | Row stacking/rounding tightness (if needed) |
| `internal/workers/media_worker.go` | Modified | 0.35s cue sample delay |
| `internal/streamclips/*_test.go` | Modified | Regression tests |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Stricter filters reject real notices (thin/short feeds) | Med | Reuse proven demo-probe constants; regression tests with real-notice geometries |
| Sample delay misses very short notices | Low | 0.35s matches proven demo-path constant; time-gate already starts cue−0.35s |
| Tighter hint bounding breaks users with off-target hints | Low | Keep small bounded slop; default hint rect still covers killfeed |

## Rollback Plan

Pure code change, no schema/artifact format change: revert the commit(s). Previously rendered clips are unaffected; re-render picks up old behavior after revert.

## Dependencies

- None (no new dependencies; reuses existing FFmpeg pipeline).

## Success Criteria

- [ ] Detection never returns rows outside the user hint rect (+slop) — covered by regression test.
- [ ] HUD avatar/score rows in synthetic fixtures are rejected by shape filters.
- [ ] Cue frame extraction uses cue + 0.35s in the stream path.
- [ ] `make test` passes; expected diff well under 400 changed lines.
