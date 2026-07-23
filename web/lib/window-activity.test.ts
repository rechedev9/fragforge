import test from 'node:test';
import assert from 'node:assert/strict';
import { focusAfterVisibilityChange, pausePlayingMedia, type PausableMedia } from './window-activity.ts';

function mediaElement(paused: boolean): PausableMedia & { pauseCalls: number } {
  let isPaused = paused;
  let pauseCalls = 0;
  return {
    get paused() {
      return isPaused;
    },
    get pauseCalls() {
      return pauseCalls;
    },
    pause() {
      pauseCalls += 1;
      isPaused = true;
    },
  };
}

test('pauses only media that is actively playing', () => {
  const playingVideo = mediaElement(false);
  const playingAudio = mediaElement(false);
  const idleMedia = mediaElement(true);

  assert.equal(pausePlayingMedia([playingVideo, idleMedia, playingAudio]), 2);
  assert.equal(playingVideo.pauseCalls, 1);
  assert.equal(playingAudio.pauseCalls, 1);
  assert.equal(idleMedia.pauseCalls, 0);
});

test('is idempotent after active media has been paused', () => {
  const media = mediaElement(false);

  assert.equal(pausePlayingMedia([media]), 1);
  assert.equal(pausePlayingMedia([media]), 0);
  assert.equal(media.pauseCalls, 1);
});

test('recomputes focus when a hidden document becomes visible', () => {
  assert.equal(focusAfterVisibilityChange('hidden', true), false);
  assert.equal(focusAfterVisibilityChange('visible', true), true);
  assert.equal(focusAfterVisibilityChange('visible', false), false);
});
