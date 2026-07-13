# Delta for streamclip-killfeed-overlay

## Purpose

Detection, cropping, and cue-frame sampling behavior of the stream-clip "clean killfeed" overlay: only real kill notices, tightly cropped, sampled when the notice highlight is fully visible.

## ADDED Requirements

### Requirement: Detection Search Region Bounded To User Hint

Killfeed notice detection MUST restrict its search region to the user-provided hint rectangle expanded by a small bounded slop. The search region MUST NOT extend to the frame's right edge or otherwise grow beyond the hint plus slop, on any axis.

#### Scenario: Rows outside hint rect are never returned

- GIVEN a frame with a valid notice inside the hint rect and notice-like pixels outside it
- WHEN detection runs with that hint
- THEN every returned row lies fully within the hint rect plus slop
- AND the outside pixels produce no rows

#### Scenario: Top-HUD band above the hint is excluded

- GIVEN a frame whose top-HUD band (score/timer, avatar rows) sits above the hint rect
- WHEN detection runs
- THEN no returned row overlaps the top-HUD band

### Requirement: HUD False-Positive Shape Filters

Detection MUST reject candidate rows that fail notice-shape filters equivalent to the demo-editor probe constants: highlight aspect ratio (width/height) below 2, highlight fill ratio above 0.5, or highlight height above frame height divided by 12.

#### Scenario: Avatar/score HUD row rejected

- GIVEN a synthetic candidate row with near-square aspect or dense fill (avatar/score geometry)
- WHEN shape filters are applied
- THEN the row is rejected and absent from detection output

#### Scenario: Real notice geometry accepted

- GIVEN a synthetic row matching real kill-notice geometry (wide, thin, sparse highlight)
- WHEN shape filters are applied
- THEN the row is accepted

#### Scenario: No candidates survive filtering

- GIVEN a frame where every candidate row fails the shape filters
- WHEN detection runs
- THEN detection returns zero rows without error

### Requirement: Cue Frame Sampling Delay

Per-cue frame extraction in the stream media path MUST sample the source video at the cue time plus 0.35 seconds, so the notice highlight ring is fully faded in. The overlay display time gate MUST continue to cover at least cue−0.35s through cue+2.8s.

#### Scenario: Extraction timestamp offset

- GIVEN a stream clip with a cue at second T
- WHEN the media worker extracts the cue frame
- THEN the extraction timestamp is T + 0.35s

#### Scenario: Overlay still visible at cue time

- GIVEN a rendered clip with a cue at second T
- WHEN the composed overlay timing is inspected
- THEN the notice overlay is displayed within the window starting no later than T−0.35s and ending no earlier than T+2.8s

### Requirement: Tight Notice Crop Bounds

Detected notice bounds MUST NOT extend more than 1 pixel beyond the notice's highlight pixels on any side. The rendered overlay strip MUST contain only detected notice rows, each scaled to the fixed row output height, right-aligned with the existing margin.

#### Scenario: Bounds hug the notice

- GIVEN a synthetic frame with a notice of known pixel extents
- WHEN detection computes the notice box
- THEN each box edge is within 1px of the true notice extents

#### Scenario: Strip contains only notices

- GIVEN a cue frame containing two real notices and HUD content
- WHEN the overlay strip is composed
- THEN the strip stacks exactly two rows and no HUD content

## Acceptance Criteria

- [ ] Regression test proves detection never returns rows outside hint rect + slop.
- [ ] Regression tests prove HUD avatar/score geometries are rejected and real-notice geometries accepted.
- [ ] Test proves stream-path cue extraction samples at cue + 0.35s.
- [ ] Test proves box bounds stay within 1px of synthetic notice extents.
- [ ] `make test` passes.
