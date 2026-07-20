import type { KillfeedKill, NormalizedRect, StreamClipRange, StreamTextOverlay, StreamVariant } from './api/streams';

export type FrameSize = { width: number; height: number };

export type CropCoverGeometry = {
  widthPercent: number;
  heightPercent: number;
  leftPercent: number;
  topPercent: number;
};

export type MontagePlaybackCursor = {
  clipIndex: number;
  sourceSeconds: number;
  playbackRate: number;
};

function playableClip(clip: StreamClipRange): boolean {
  return Number.isFinite(clip.start_seconds) &&
    Number.isFinite(clip.end_seconds) &&
    clip.start_seconds >= 0 &&
    clip.end_seconds > clip.start_seconds;
}

function clipPlaybackRate(clip: StreamClipRange): number {
  const speed = clip.edit?.speed ?? 1;
  return Number.isFinite(speed) && speed > 0 ? speed : 1;
}

/** Starts montage playback at the selected clip, never in an excluded source gap. */
export function startMontagePlayback(
  clips: StreamClipRange[],
  sourceSeconds: number,
): MontagePlaybackCursor | null {
  const firstIndex = clips.findIndex(playableClip);
  if (firstIndex < 0) return null;
  const selectedIndex = clips.findIndex((clip) =>
    playableClip(clip) && sourceSeconds >= clip.start_seconds && sourceSeconds < clip.end_seconds,
  );
  const clipIndex = selectedIndex >= 0 ? selectedIndex : firstIndex;
  const clip = clips[clipIndex];
  return {
    clipIndex,
    sourceSeconds: selectedIndex >= 0 ? sourceSeconds : clip.start_seconds,
    playbackRate: clipPlaybackRate(clip),
  };
}

/** Advances within one clip or jumps to the next edited range; null means montage end. */
export function advanceMontagePlayback(
  clips: StreamClipRange[],
  clipIndex: number,
  sourceSeconds: number,
): MontagePlaybackCursor | null {
  const clip = clips[clipIndex];
  if (clip && playableClip(clip) && sourceSeconds < clip.end_seconds) {
    return { clipIndex, sourceSeconds, playbackRate: clipPlaybackRate(clip) };
  }
  for (let index = clipIndex + 1; index < clips.length; index++) {
    const next = clips[index];
    if (!playableClip(next)) continue;
    return {
      clipIndex: index,
      sourceSeconds: next.start_seconds,
      playbackRate: clipPlaybackRate(next),
    };
  }
  return null;
}

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

const KILLFEED_WIDTH = 930;
const KILLFEED_SAMPLE_DELAY_SECONDS = 0.35;
const KILLFEED_SAMPLE_END_GUARD_SECONDS = 0.05;
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
      const visibleFrom = cue;
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

/**
 * Returns the later source frame used to read/freeze a cue while keeping the
 * cue itself as the exact instant the rendered notice becomes visible.
 */
export function killfeedSampleFrameSeconds(clips: StreamClipRange[], cue: number): number {
  for (const clip of clips) {
    if ((clip.killfeed_seconds ?? []).includes(cue)) {
      return Math.min(
        cue + KILLFEED_SAMPLE_DELAY_SECONDS,
        Math.max(cue, clip.end_seconds - KILLFEED_SAMPLE_END_GUARD_SECONDS),
      );
    }
  }
  return cue;
}

// Synthetic notice stack geometry, in output pixels on the 1080x1920 canvas,
// mirroring the render: 72px-tall notices horizontally centered, stacked upward
// from a base top with an 8px gap between them (later concurrent notices sit
// above earlier ones). The base top is a fixed fraction down the gameplay band.
const KILLFEED_OUTPUT_HEIGHT = 1920;
const KILLFEED_NOTICE_HEIGHT = 72;
const KILLFEED_NOTICE_GAP = 8;
const KILLFEED_GAMEPLAY_TOP_FRACTION = 0.24;

export type KillfeedNoticePlacement = {
  topPercent: number;
  heightPercent: number;
};

/**
 * Top of the killfeed in 1080x1920 output pixels, mirroring killfeedBaseY in
 * internal/streamclips/ffmpeg.go: a fixed fraction down the gameplay band.
 * `gameplayTopPixels` is the facecam band height (0 for full-frame variants),
 * and `gameplayHeightPixels` is the gameplay band height.
 */
export function killfeedBaseTopPixels(gameplayTopPixels: number, gameplayHeightPixels: number): number {
  return gameplayTopPixels + Math.round(KILLFEED_GAMEPLAY_TOP_FRACTION * gameplayHeightPixels);
}

/**
 * Placement of the notice at stack position `index` (0 = base), as percentages
 * of the preview box so it scales with the box. `baseTopPixels` is the same base
 * top the frozen-crop overlay uses. Notices stack upward, so slot `index` sits
 * `index` steps above the base. Horizontal centering is a fixed CSS concern the
 * component handles; the render also adds slide-in/fade entrance and exit
 * animation the static preview intentionally omits.
 */
export function killfeedNoticePlacement(index: number, baseTopPixels: number): KillfeedNoticePlacement {
  const topPixels = baseTopPixels - index * (KILLFEED_NOTICE_HEIGHT + KILLFEED_NOTICE_GAP);
  return {
    topPercent: (topPixels * 100) / KILLFEED_OUTPUT_HEIGHT,
    heightPercent: (KILLFEED_NOTICE_HEIGHT * 100) / KILLFEED_OUTPUT_HEIGHT,
  };
}

/**
 * The confirmed kills for the cue timestamp `cue`, found by its index in the
 * owning clip's `killfeed_seconds`. Returns an empty array when the cue is not
 * marked or has no kills, so callers fall back to the frozen-crop overlay.
 */
export function killfeedKillsForCue(clips: StreamClipRange[], cue: number): KillfeedKill[] {
  for (const clip of clips) {
    const index = (clip.killfeed_seconds ?? []).indexOf(cue);
    if (index < 0) continue;
    const kills = clip.killfeed_kills?.[index];
    if (kills && kills.length > 0) return kills;
  }
  return [];
}

/** Selects the same stable, representative frame for every editor video. */
export function representativeFrameTime(duration: number): number {
  if (!Number.isFinite(duration) || duration <= 0) return 0;
  return Math.max(0, Math.min(duration / 2, duration - 0.1));
}

/**
 * The text overlays visible at `frameSeconds`. Overlay windows are relative to
 * the owning clip's start in source seconds (matching the render's drawtext
 * enable windows); missing bounds extend to the clip edges.
 */
export function activeTextOverlays(
  clips: StreamClipRange[],
  frameSeconds: number,
): StreamTextOverlay[] {
  if (!Number.isFinite(frameSeconds)) return [];
  const active: StreamTextOverlay[] = [];
  for (const clip of clips) {
    if (frameSeconds < clip.start_seconds || frameSeconds >= clip.end_seconds) continue;
    const relative = frameSeconds - clip.start_seconds;
    for (const overlay of clip.edit?.text_overlays ?? []) {
      if (overlay.start_seconds !== undefined && relative < overlay.start_seconds) continue;
      if (overlay.end_seconds !== undefined && relative > overlay.end_seconds) continue;
      active.push(overlay);
    }
  }
  return active;
}
