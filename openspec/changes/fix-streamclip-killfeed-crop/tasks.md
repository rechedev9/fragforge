# Tasks: Fix Stream Clip Killfeed Overlay Crop

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 120-180 |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | auto-forecast |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Correct stream killfeed detection, crop, and sampling with regressions | PR 1 | `go test ./internal/streamclips ./internal/workers ./cmd/zv-orchestrator -count=1` | N/A: synthetic image and worker command tests cover the boundary without launching FFmpeg/CS2 | Revert `killfeed_detect.go`, `ffmpeg.go`, `media_worker.go`, and their two changed test files |

## Phase 1: RED Regression Tests

- [x] 1.1 In `internal/streamclips/killfeed_detect_test.go`, repurpose the frame-right-edge test and add cases proving inside-hint notices pass while outside/higher HUD pixels beyond hint ±8px return no rows.
- [x] 1.2 In `internal/streamclips/killfeed_detect_test.go`, add aspect, strict-fill, and max-height rejection cases; prove real, compressed, and mixed-style notices pass and all-rejected input returns zero rows without error.
- [x] 1.3 In `internal/streamclips/killfeed_detect_test.go`, add a 1px-bound helper and a two-notice-plus-HUD case proving exact notice-only rows and tight known extents.
- [x] 1.4 In `internal/workers/stream_worker_test.go`, expect cue extraction `-ss` values `0.850` and `1.600` while warnings retain original cues. Move only the second-cue failure trigger near line 229 from `1.250` to `1.600`; keep caption-audio `1.250` near line 437 unchanged.

## Phase 2: GREEN Implementation

- [x] 2.1 In `internal/streamclips/killfeed_detect.go`, clamp `noticeSearchRegion` to every hint edge plus `killfeedSearchPadding` and frame bounds; preserve the nil-hint default.
- [x] 2.2 In `internal/streamclips/killfeed_detect.go`, filter paired boxes before dedupe using aspect ≥2, strict-red fill ≤0.5, and height ≤ frame/12; document constants as mirrors of `internal/editor/killfeed_probe.go`.
- [x] 2.3 In `internal/streamclips/killfeed_detect.go`, set `killfeedLooseEdgeReach` to 1 while retaining the 1px row margin.
- [x] 2.4 In `internal/streamclips/ffmpeg.go`, add documented `KillfeedSampleDelaySeconds = 0.35`; in `internal/workers/media_worker.go`, add it only to extraction `-ss`, leaving original-cue warnings and overlay gates unchanged.

## Phase 3: Verification

- [x] 3.1 Run the focused command and verify `cmd/zv-orchestrator/stream_e2e_test.go`: hint `{0.74,0.04,0.25,0.15}` near line 131, cue `[2]` near 136, and 1280x720 source near 297 remain valid without assertion changes.
- [x] 3.2 Verify existing `internal/streamclips` FFmpeg tests still prove fixed-height, right-aligned notice stacking and the `cue−0.35` through `cue+2.8` gate; do not change `buildKillfeedFilterGraph`.
- [x] 3.3 Run `make test` and record the exact result before completing the single work unit.
