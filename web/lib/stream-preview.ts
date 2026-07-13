import type { NormalizedRect, StreamClipRange, StreamVariant } from './api/streams';

export type FrameSize = { width: number; height: number };

export type CropCoverGeometry = {
  widthPercent: number;
  heightPercent: number;
  leftPercent: number;
  topPercent: number;
};

export const STREAMER_BANNER_MIN_POSITION = 0.025;
export const STREAMER_BANNER_MAX_POSITION = 0.975;

const STREAMER_BANNER_DEFAULTS: Record<StreamVariant, number> = {
  'streamer-vertical-stack-40-60': 0.374,
  'streamer-vertical-stack': 520 / 1920,
  'streamer-fullframe-nocam': 0.2,
};

export function clampStreamerBannerPosition(position: number): number {
  return Math.min(STREAMER_BANNER_MAX_POSITION, Math.max(STREAMER_BANNER_MIN_POSITION, position));
}

export function defaultStreamerBannerPosition(variant: StreamVariant): number {
  return STREAMER_BANNER_DEFAULTS[variant];
}

export function resolveStreamerBannerPosition(variant: StreamVariant, position?: number): number {
  return position === undefined
    ? defaultStreamerBannerPosition(variant)
    : clampStreamerBannerPosition(position);
}

/**
 * Mirrors FFmpeg's crop -> scale(force_original_aspect_ratio=increase) ->
 * centered crop chain for one output band.
 */
export function calculateCropCoverGeometry(
  rect: NormalizedRect,
  source: FrameSize,
  output: FrameSize,
): CropCoverGeometry | null {
  if (
    source.width <= 0 ||
    source.height <= 0 ||
    output.width <= 0 ||
    output.height <= 0 ||
    rect.width <= 0 ||
    rect.height <= 0
  ) {
    return null;
  }

  const cropWidth = source.width * rect.width;
  const cropHeight = source.height * rect.height;
  const scale = Math.max(output.width / cropWidth, output.height / cropHeight);
  const scaledCropWidth = cropWidth * scale;
  const scaledCropHeight = cropHeight * scale;

  return {
    widthPercent: (source.width * scale * 100) / output.width,
    heightPercent: (source.height * scale * 100) / output.height,
    leftPercent: (((output.width - scaledCropWidth) / 2 - source.width * rect.x * scale) * 100) / output.width,
    topPercent: (((output.height - scaledCropHeight) / 2 - source.height * rect.y * scale) * 100) / output.height,
  };
}

const KILLFEED_WIDTH = 620;
const KILLFEED_LEAD_SECONDS = 0.35;
const KILLFEED_TAIL_SECONDS = 2.8;

export function proportionalEvenKillfeedHeight(
  rect: NormalizedRect,
  source: FrameSize,
): number | null {
  if (rect.width <= 0 || rect.height <= 0 || source.width <= 0 || source.height <= 0) {
    return null;
  }
  const proportionalHeight =
    (KILLFEED_WIDTH * rect.height * source.height) / (rect.width * source.width);
  return Math.max(2, Math.round(proportionalHeight / 2) * 2);
}

export function resolveActiveKillfeedCue(
  clips: StreamClipRange[],
  frameSeconds: number,
): number | null {
  if (!Number.isFinite(frameSeconds)) return null;
  let activeCue: number | null = null;
  for (const clip of clips) {
    for (const cue of clip.killfeed_seconds ?? []) {
      if (
        !Number.isFinite(cue) ||
        cue < clip.start_seconds ||
        cue >= clip.end_seconds
      ) {
        continue;
      }
      const visibleFrom = Math.max(clip.start_seconds, cue - KILLFEED_LEAD_SECONDS);
      const visibleThrough = Math.min(clip.end_seconds, cue + KILLFEED_TAIL_SECONDS);
      if (
        frameSeconds >= visibleFrom &&
        frameSeconds < clip.end_seconds &&
        frameSeconds <= visibleThrough &&
        (activeCue === null || cue >= activeCue)
      ) {
        activeCue = cue;
      }
    }
  }
  return activeCue;
}

/** Selects the same stable, representative frame for every editor video. */
export function representativeFrameTime(duration: number): number {
  if (!Number.isFinite(duration) || duration <= 0) return 0;
  return Math.max(0, Math.min(duration / 2, duration - 0.1));
}
