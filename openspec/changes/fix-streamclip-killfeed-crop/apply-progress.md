# Apply Progress: Fix Stream Clip Killfeed Overlay Crop

## Status

All tasks in phases 1 through 3 are complete. Implementation mode: Strict TDD. Delivery mode: single PR, one work unit.

## Completed Tasks

- [x] 1.1-1.4 RED regression tests
- [x] 2.1-2.4 GREEN implementation
- [x] 3.1-3.3 focused, runtime, and full-gate verification

## TDD Cycle Evidence

| Tasks | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 / 2.1 | `internal/streamclips/killfeed_detect_test.go` | Unit | Focused baseline passed 3 packages | Outside-right notice returned one row | Bounded hint test passed | Inside, right-outside, and above-hint cases | Region math kept in `noticeSearchRegion` |
| 1.2 / 2.2 | `internal/streamclips/killfeed_detect_test.go` | Unit | Focused baseline passed 3 packages | Near-square and dense HUD rows were returned | Shape rejection tests passed | Aspect, fill, height, real, compressed, and mixed styles | Filter isolated before dedupe; constants documented |
| 1.3 / 2.3 | `internal/streamclips/killfeed_detect_test.go` | Unit | Focused baseline passed 3 packages | Three rows returned, including HUD | Exactly two tight notice rows passed | Two notices plus dense HUD and loose edge spill | Loose reach reduced while retaining row margin |
| 1.4 / 2.4 | `internal/workers/stream_worker_test.go` | Integration | Focused baseline passed 3 packages | Extraction used `0.500`, expected `0.850` | Extraction used `0.850` and `1.600`; warnings retained original cues | Two distinct cues and failure warning path | Shared documented delay constant; overlay graph unchanged |

## Test Summary

- Total regression scenarios added: 7
- Focused GREEN: `go test ./internal/streamclips ./internal/workers ./cmd/zv-orchestrator -count=1` passed all three packages.
- Runtime boundary: `TestStreamRenderE2E` passed through the real HTTP/worker/FFmpeg path without assertion changes.
- Full gate: `make test` passed via GNU Make with `SHELL=pwsh.exe`; all Go packages passed and `zv check` reported `OK: 6 skills, 14 workflows, 11 workflow docs, and 19 agent prompt wrappers checked`.
- Pure functions created: 1 (`filterNoticeBoxes`).
- Approval tests: None; this is a behavior correction covered by RED regressions.

## Work Unit Evidence

| Evidence | Result |
|---|---|
| Focused test command and exact result | `go test ./internal/streamclips ./internal/workers ./cmd/zv-orchestrator -count=1` exited 0: all three packages `ok`. |
| Runtime harness command/scenario and exact result | The focused command executed `cmd/zv-orchestrator`'s real stream-render E2E package and exited 0; the unchanged staggered killfeed scenario passed. |
| Rollback boundary | Revert only the killfeed-crop changes in `internal/streamclips/killfeed_detect.go`, `internal/streamclips/killfeed_detect_test.go`, `internal/streamclips/ffmpeg.go`, `internal/workers/media_worker.go`, and `internal/workers/stream_worker_test.go`. |

## Deviations

None. `buildKillfeedFilterGraph`, its original-cue time gate, the caption-audio `1.250` assertion, and the existing E2E assertions were not changed.

## Scoped Correction: Bounded Final Row Expansion

The edge regression failed first with row `(1319,73)-(1815,110)` exceeding padded hint region `(1297,46)-(1813,214)`. Final row-margin expansion now intersects the bounded search region and the regression passes.

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| Clamp final expanded row | `internal/streamclips/killfeed_detect_test.go` | Unit | Focused baseline passed both packages | Edge-touching notice exceeded region by 2px | Edge case and focused suite passed | Existing inside/outside/above-hint cases also passed | Minimal one-line clamp; no further refactor |

| Evidence | Result |
|---|---|
| Focused test command and exact result | `go test ./internal/streamclips ./internal/workers -count=1` exited 0; both packages `ok`. |
| Runtime harness command/scenario and exact result | `go test ./cmd/zv-orchestrator -count=1` exited 0; package `ok`. |
| Rollback boundary | Revert this correction in `internal/streamclips/killfeed_detect.go` and its edge regression in `internal/streamclips/killfeed_detect_test.go`. |
