import test from 'node:test';
import assert from 'node:assert/strict';
import {
  STREAMER_BANNER_MAX_POSITION,
  STREAMER_BANNER_MIN_POSITION,
  calculateCropCoverGeometry,
  clampStreamerBannerPosition,
  defaultStreamerBannerPosition,
  killfeedKillsForCue,
  killfeedNoticePlacement,
  proportionalEvenKillfeedHeight,
  resolveActiveKillfeedCue,
  representativeFrameTime,
  resolveStreamerBannerPosition,
} from './stream-preview.ts';
import type { KillfeedKill } from './api/streams.ts';

const KILL: KillfeedKill = {
  attacker_side: 'CT',
  attacker_name: 'hero',
  victim_side: 'T',
  victim_name: 'villain',
  weapon: 'ak47',
};

const SOURCE = { width: 1920, height: 1080 };

test('full-frame gameplay covers the 40/60 gameplay band without stretching', () => {
  const geometry = calculateCropCoverGeometry(
    { x: 0, y: 0, width: 1, height: 1 },
    SOURCE,
    { width: 1080, height: 1152 },
  );

  assert.ok(geometry);
  const expectedWidthPercent = (SOURCE.width * (1152 / SOURCE.height) * 100) / 1080;
  assert.equal(geometry.heightPercent, 100);
  assert.equal(geometry.widthPercent, expectedWidthPercent);
  assert.ok(Math.abs(geometry.leftPercent - (100 - expectedWidthPercent) / 2) < 1e-12);
  assert.equal(geometry.topPercent, 0);

  const displayedAspect = (geometry.widthPercent * 1080) / (geometry.heightPercent * 1152);
  assert.equal(displayedAspect, SOURCE.width / SOURCE.height);
});

test('reported facecam crop stays centered and aspect-correct in its output band', () => {
  const rect = {
    x: 0.0414141414,
    y: 0.2711560045,
    width: 0.1863636364,
    height: 0.2066217733,
  };
  const output = { width: 1080, height: 768 };
  const geometry = calculateCropCoverGeometry(rect, SOURCE, output);

  assert.ok(geometry);
  const scaleFromWidth = (geometry.widthPercent / 100 * output.width) / SOURCE.width;
  const scaleFromHeight = (geometry.heightPercent / 100 * output.height) / SOURCE.height;
  assert.ok(Math.abs(scaleFromWidth - scaleFromHeight) < 1e-12);

  const scaledCropWidth = SOURCE.width * rect.width * scaleFromWidth;
  const scaledCropHeight = SOURCE.height * rect.height * scaleFromHeight;
  assert.ok(scaledCropWidth >= output.width);
  assert.ok(scaledCropHeight >= output.height);
  assert.ok(Math.abs(scaledCropHeight - output.height) < 1e-9);

  const visibleCenterX = (-geometry.leftPercent / 100 * output.width + output.width / 2) / scaleFromWidth;
  const visibleCenterY = (-geometry.topPercent / 100 * output.height + output.height / 2) / scaleFromHeight;
  assert.ok(Math.abs(visibleCenterX - SOURCE.width * (rect.x + rect.width / 2)) < 1e-9);
  assert.ok(Math.abs(visibleCenterY - SOURCE.height * (rect.y + rect.height / 2)) < 1e-9);
});

test('legacy 520/1400 bands cover without changing the source aspect ratio', () => {
  for (const output of [
    { width: 1080, height: 520 },
    { width: 1080, height: 1400 },
  ]) {
    const geometry = calculateCropCoverGeometry(
      { x: 0, y: 0, width: 1, height: 1 },
      SOURCE,
      output,
    );

    assert.ok(geometry);
    const displayedWidth = geometry.widthPercent / 100 * output.width;
    const displayedHeight = geometry.heightPercent / 100 * output.height;
    assert.equal(displayedWidth / displayedHeight, SOURCE.width / SOURCE.height);
    assert.ok(displayedWidth >= output.width);
    assert.ok(displayedHeight >= output.height);
  }
});

test('killfeed overlay uses a proportional nearest-even height for its 620px output', () => {
  assert.equal(
    proportionalEvenKillfeedHeight(
      { x: 0.68, y: 0.04, width: 0.31, height: 0.14 },
      SOURCE,
    ),
    158,
  );
  assert.equal(
    proportionalEvenKillfeedHeight(
      { x: 0, y: 0, width: 1, height: 1 },
      SOURCE,
    ),
    348,
  );
  assert.equal(
    proportionalEvenKillfeedHeight(
      { x: 0, y: 0, width: 1, height: 0.001 },
      SOURCE,
    ),
    2,
  );
});

test('killfeed overlay rejects non-positive crop and source geometry', () => {
  const crop = { x: 0.68, y: 0.04, width: 0.31, height: 0.14 };

  assert.equal(
    proportionalEvenKillfeedHeight({ ...crop, width: 0 }, SOURCE),
    null,
  );
  assert.equal(
    proportionalEvenKillfeedHeight({ ...crop, height: -0.1 }, SOURCE),
    null,
  );
  assert.equal(
    proportionalEvenKillfeedHeight(crop, { width: 0, height: SOURCE.height }),
    null,
  );
  assert.equal(
    proportionalEvenKillfeedHeight(crop, { width: SOURCE.width, height: -1 }),
    null,
  );
});

test('killfeed cues use half-open clip boundaries', () => {
  const clips = [{
    id: 'half-open',
    start_seconds: 10,
    end_seconds: 20,
    killfeed_seconds: [10, 20],
  }];

  assert.equal(resolveActiveKillfeedCue(clips, 9.999), null);
  assert.equal(resolveActiveKillfeedCue(clips, 10), 10);
  assert.equal(resolveActiveKillfeedCue(clips, 19.999), null);
  assert.equal(resolveActiveKillfeedCue(clips, 20), null);
});

test('killfeed visibility clips lead and trail windows to the clip', () => {
  const clips = [{
    id: 'clipped-window',
    start_seconds: 10,
    end_seconds: 12,
    killfeed_seconds: [10.2],
  }];

  assert.equal(resolveActiveKillfeedCue(clips, 9.999), null);
  assert.equal(resolveActiveKillfeedCue(clips, 10), 10.2);
  assert.equal(resolveActiveKillfeedCue(clips, 12), null);
  assert.equal(resolveActiveKillfeedCue(clips, 12.001), null);
});

test('killfeed cue resolution ignores invalid cues and frame times', () => {
  const clips = [{
    id: 'invalid-cues',
    start_seconds: 5,
    end_seconds: 10,
    killfeed_seconds: [
      Number.NaN,
      Number.NEGATIVE_INFINITY,
      Number.POSITIVE_INFINITY,
      4.999,
      10,
      7,
    ],
  }];

  assert.equal(resolveActiveKillfeedCue(clips, 7), 7);
  assert.equal(resolveActiveKillfeedCue(clips, 9.8), 7);
  assert.equal(resolveActiveKillfeedCue(clips, 9.801), null);
  assert.equal(resolveActiveKillfeedCue(clips, Number.NaN), null);
  assert.equal(resolveActiveKillfeedCue(clips, Number.POSITIVE_INFINITY), null);
});

test('overlapping killfeed windows select the latest cue timestamp', () => {
  const clips = [{
    id: 'overlap',
    start_seconds: 0,
    end_seconds: 20,
    killfeed_seconds: [12, 10, 11],
  }];

  assert.equal(resolveActiveKillfeedCue(clips, 11.649), 11);
  assert.equal(resolveActiveKillfeedCue(clips, 11.65), 12);
  assert.equal(resolveActiveKillfeedCue(clips, 12.8), 12);
});

test('killfeed cue resolution uses absolute source time across multiple clips', () => {
  const clips = [
    {
      id: 'first',
      start_seconds: 0,
      end_seconds: 5,
      killfeed_seconds: [1],
    },
    {
      id: 'second',
      start_seconds: 20,
      end_seconds: 25,
      killfeed_seconds: [22],
    },
  ];

  assert.equal(resolveActiveKillfeedCue(clips, 0.649), null);
  assert.equal(resolveActiveKillfeedCue(clips, 0.65), 1);
  assert.equal(resolveActiveKillfeedCue(clips, 3.8), 1);
  assert.equal(resolveActiveKillfeedCue(clips, 15), null);
  assert.equal(resolveActiveKillfeedCue(clips, 21.65), 22);
  assert.equal(resolveActiveKillfeedCue(clips, 24.8), 22);
  assert.equal(resolveActiveKillfeedCue(clips, 24.801), null);
});

test('synthetic notice placement stacks 48px notices with an 8px gap from the base top', () => {
  const first = killfeedNoticePlacement(0, 64);
  assert.equal(first.heightPercent, (48 * 100) / 1920);
  assert.equal(first.rightPercent, (24 * 100) / 1080);
  assert.equal(first.topPercent, (64 * 100) / 1920);

  const second = killfeedNoticePlacement(1, 64);
  assert.equal(second.topPercent, ((64 + 48 + 8) * 100) / 1920);
  assert.equal(second.heightPercent, first.heightPercent);
  assert.equal(second.rightPercent, first.rightPercent);

  const third = killfeedNoticePlacement(2, 64);
  assert.equal(third.topPercent, ((64 + 2 * (48 + 8)) * 100) / 1920);
});

test('synthetic notice placement honors a stacked base top offset', () => {
  const stacked = killfeedNoticePlacement(0, 768 + 72);
  assert.equal(stacked.topPercent, ((768 + 72) * 100) / 1920);
});

test('killfeedKillsForCue returns the kills index-aligned with the matching cue', () => {
  const other: KillfeedKill = { ...KILL, attacker_name: 'second', weapon: 'awp' };
  const clips = [
    {
      id: 'clip-1',
      start_seconds: 0,
      end_seconds: 20,
      killfeed_seconds: [4, 8, 12],
      killfeed_kills: [[KILL], [], [KILL, other]],
    },
  ];

  assert.deepEqual(killfeedKillsForCue(clips, 4), [KILL]);
  assert.deepEqual(killfeedKillsForCue(clips, 8), []);
  assert.deepEqual(killfeedKillsForCue(clips, 12), [KILL, other]);
});

test('killfeedKillsForCue returns empty when the cue or kills are absent', () => {
  const clips = [
    { id: 'no-kills', start_seconds: 0, end_seconds: 20, killfeed_seconds: [4] },
    { id: 'with-kills', start_seconds: 20, end_seconds: 40, killfeed_seconds: [24], killfeed_kills: [[KILL]] },
  ];

  assert.deepEqual(killfeedKillsForCue(clips, 4), []);
  assert.deepEqual(killfeedKillsForCue(clips, 99), []);
  assert.deepEqual(killfeedKillsForCue(clips, 24), [KILL]);
});

test('representative time is the safe midpoint for every editor video', () => {
  assert.equal(representativeFrameTime(42), 21);
  assert.equal(representativeFrameTime(0.05), 0);
  assert.equal(representativeFrameTime(0), 0);
  assert.equal(representativeFrameTime(Number.POSITIVE_INFINITY), 0);
});

test('streamer banner defaults follow each output layout', () => {
  assert.equal(defaultStreamerBannerPosition('streamer-vertical-stack-40-60'), 0.374);
  assert.equal(defaultStreamerBannerPosition('streamer-vertical-stack'), 520 / 1920);
  assert.equal(defaultStreamerBannerPosition('streamer-fullframe-nocam'), 0.2);
});

test('explicit streamer banner position stays absolute across layouts', () => {
  for (const variant of [
    'streamer-vertical-stack-40-60',
    'streamer-vertical-stack',
    'streamer-fullframe-nocam',
  ] as const) {
    assert.equal(resolveStreamerBannerPosition(variant, 0.73), 0.73);
  }
});

test('streamer banner position clamps to keep the strip fully visible', () => {
  assert.equal(clampStreamerBannerPosition(-1), STREAMER_BANNER_MIN_POSITION);
  assert.equal(clampStreamerBannerPosition(STREAMER_BANNER_MIN_POSITION), STREAMER_BANNER_MIN_POSITION);
  assert.equal(clampStreamerBannerPosition(0.5), 0.5);
  assert.equal(clampStreamerBannerPosition(STREAMER_BANNER_MAX_POSITION), STREAMER_BANNER_MAX_POSITION);
  assert.equal(clampStreamerBannerPosition(2), STREAMER_BANNER_MAX_POSITION);
});

test('undefined streamer banner position resets to the current layout default', () => {
  assert.equal(resolveStreamerBannerPosition('streamer-vertical-stack-40-60', undefined), 0.374);
  assert.equal(resolveStreamerBannerPosition('streamer-vertical-stack', undefined), 520 / 1920);
  assert.equal(resolveStreamerBannerPosition('streamer-fullframe-nocam', undefined), 0.2);
});
