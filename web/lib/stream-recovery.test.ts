import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  CAPTION_GENERATION_STATUS,
  STREAM_RENDER_ERROR_CODE,
  type CaptionGenerationState,
  type StreamRenderState,
} from './api/streams.ts';
import {
  canRequestCaptionCandidates,
  streamRenderCanRetry,
  streamRenderNeedsKillfeedReanalysis,
} from './stream-recovery.ts';

function captionState(status: CaptionGenerationState['status']): CaptionGenerationState {
  return {
    job_id: '11111111-1111-1111-1111-111111111111',
    generation_id: '22222222-2222-2222-2222-222222222222',
    status,
    clips: [],
    updated_at: '2026-07-18T00:00:00Z',
  };
}

describe('canRequestCaptionCandidates', () => {
  it('permits replacing stale ready and review-required candidates', () => {
    assert.equal(canRequestCaptionCandidates(true, captionState(CAPTION_GENERATION_STATUS.ready)), true);
    assert.equal(canRequestCaptionCandidates(true, captionState(CAPTION_GENERATION_STATUS.reviewRequired)), true);
  });

  it('blocks only when review is unnecessary or generation is active', () => {
    assert.equal(canRequestCaptionCandidates(false, null), false);
    assert.equal(canRequestCaptionCandidates(true, captionState(CAPTION_GENERATION_STATUS.queued)), false);
    assert.equal(canRequestCaptionCandidates(true, captionState(CAPTION_GENERATION_STATUS.generating)), false);
    assert.equal(canRequestCaptionCandidates(true, captionState(CAPTION_GENERATION_STATUS.failed)), true);
  });
});

describe('streamRenderCanRetry', () => {
  it('keeps superseded and stale-artifact failures in the editor', () => {
    const failed: StreamRenderState = { status: 'failed', videos: [] };
    assert.equal(streamRenderCanRetry({ ...failed, error_code: STREAM_RENDER_ERROR_CODE.superseded }), true);
    assert.equal(streamRenderCanRetry({ ...failed, error_code: STREAM_RENDER_ERROR_CODE.killfeedArtifactsStale }), true);
    assert.equal(streamRenderCanRetry({ ...failed, published: true, error_code: 'ffmpeg_failed' }), true);
    assert.equal(streamRenderCanRetry({ ...failed, error_code: 'other' }), false);
    assert.equal(streamRenderCanRetry({ ...failed, status: 'rendering', error_code: STREAM_RENDER_ERROR_CODE.superseded }), false);
  });
});

describe('streamRenderNeedsKillfeedReanalysis', () => {
  it('recognizes only the stable recoverable killfeed failure', () => {
    const recoverable: StreamRenderState = {
      status: 'failed',
      videos: [],
      error_code: STREAM_RENDER_ERROR_CODE.killfeedArtifactsStale,
    };
    assert.equal(streamRenderNeedsKillfeedReanalysis(recoverable), true);
    assert.equal(streamRenderNeedsKillfeedReanalysis({ ...recoverable, error_code: 'other' }), false);
    assert.equal(streamRenderNeedsKillfeedReanalysis({ ...recoverable, status: 'rendering' }), false);
    assert.equal(streamRenderNeedsKillfeedReanalysis(null), false);
  });
});
