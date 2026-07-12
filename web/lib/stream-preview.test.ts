import test from 'node:test';
import assert from 'node:assert/strict';
import {
  STREAMER_BANNER_MAX_POSITION,
  STREAMER_BANNER_MIN_POSITION,
  calculateCropCoverGeometry,
  clampStreamerBannerPosition,
  defaultStreamerBannerPosition,
  representativeFrameTime,
  resolveStreamerBannerPosition,
} from './stream-preview.ts';

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
