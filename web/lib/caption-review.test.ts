import test from 'node:test';
import assert from 'node:assert/strict';
import {
  captionDraftDiffersFromReview,
  captionInputsFingerprint,
  captionsNeedReview,
  captionWordsIssue,
  invalidateCaptionReview,
  streamHasAudio,
} from './caption-review.ts';
import type { StreamEditPlan } from './api/streams.ts';

function plan(): StreamEditPlan {
  return {
    schema_version: '1.1',
    variant: 'streamer-fullframe-nocam',
    captions: { enabled: true, language: 'es' },
    clips: [
      { id: 'one', start_seconds: 0, end_seconds: 4 },
      { id: 'two', start_seconds: 4, end_seconds: 8, edit: { source_volume: 0 } },
    ],
  };
}

test('caption gate requires review only for audible clips with source audio', () => {
  const current = plan();
  assert.equal(captionsNeedReview(current), true);
  current.clips[0].caption_reviewed = true;
  assert.equal(captionsNeedReview(current), false);
  current.clips[0].caption_reviewed = false;
  assert.equal(captionsNeedReview(current, false), false);
});

test('an omitted audio codec means the source has no audio track', () => {
  assert.equal(streamHasAudio({ width: 1920, height: 1080, duration_seconds: 10 }), false);
  assert.equal(streamHasAudio({ width: 1920, height: 1080, duration_seconds: 10, audio_codec: 'aac' }), true);
});

test('caption input changes are fingerprinted and review can be invalidated', () => {
  const current = plan();
  current.clips[0].caption_reviewed = true;
  current.clips[0].caption_words = [{ word: 'hola', start_seconds: 0, end_seconds: 0.4 }];
  const before = captionInputsFingerprint(current.clips);
  current.clips[0].edit = { speed: 0.75 };
  assert.notEqual(captionInputsFingerprint(current.clips), before);
  assert.deepEqual(invalidateCaptionReview(current.clips[0]).caption_words, undefined);
  assert.equal(invalidateCaptionReview(current.clips[0]).caption_reviewed, undefined);
});

test('editing approved caption words makes the review dirty', () => {
  const clip = plan().clips[0];
  clip.caption_reviewed = true;
  clip.caption_words = [{ word: 'hola', start_seconds: 0, end_seconds: 0.4 }];
  assert.equal(captionDraftDiffersFromReview(clip, clip.caption_words), false);
  assert.equal(
    captionDraftDiffersFromReview(clip, [{ word: 'adiós', start_seconds: 0, end_seconds: 0.4 }]),
    true,
  );
});

test('caption word validation catches overlap and accepts ordered clip-relative cues', () => {
  assert.match(
    captionWordsIssue([
      { word: 'hola', start_seconds: 0, end_seconds: 0.7 },
      { word: 'mundo', start_seconds: 0.6, end_seconds: 1 },
    ], 2) ?? '',
    /solapan/,
  );
  assert.equal(
    captionWordsIssue([
      { word: 'hola', start_seconds: 0, end_seconds: 0.5 },
      { word: 'mundo', start_seconds: 0.6, end_seconds: 1 },
    ], 2),
    null,
  );
});

test('caption word validation mirrors backend text limits', () => {
  assert.match(
    captionWordsIssue([{ word: 'a'.repeat(81), start_seconds: 0, end_seconds: 0.5 }], 1) ?? '',
    /80 caracteres/,
  );
  assert.match(
    captionWordsIssue([{ word: 'hola\nmundo', start_seconds: 0, end_seconds: 0.5 }], 1) ?? '',
    /saltos de línea/,
  );
});
