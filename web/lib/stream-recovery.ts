import {
  CAPTION_GENERATION_STATUS,
  STREAM_RENDER_ERROR_CODE,
  type CaptionGenerationState,
  type StreamRenderState,
} from './api/streams.ts';

/** Whether Studio can safely replace the current caption candidates. */
export function canRequestCaptionCandidates(
  captionsNeedReview: boolean,
  state: CaptionGenerationState | null,
): boolean {
  if (!captionsNeedReview) return false;
  return state?.status !== CAPTION_GENERATION_STATUS.queued &&
    state?.status !== CAPTION_GENERATION_STATUS.generating;
}

/**
 * Missing or corrupt exact killfeed rows are recoverable without discarding
 * the stream job. The render state code, rather than its translated message,
 * is the durable contract between the worker and Studio.
 */
export function streamRenderNeedsKillfeedReanalysis(
  state: StreamRenderState | null,
): boolean {
  return state?.status === 'failed' &&
    state.error_code === STREAM_RENDER_ERROR_CODE.killfeedArtifactsStale;
}

/** Whether a failed render can be retried from the existing editor state. */
export function streamRenderCanRetry(state: StreamRenderState | null): boolean {
  return state?.status === 'failed' && (
    state.published === true ||
    state.error_code === STREAM_RENDER_ERROR_CODE.killfeedArtifactsStale ||
    state.error_code === STREAM_RENDER_ERROR_CODE.superseded
  );
}
