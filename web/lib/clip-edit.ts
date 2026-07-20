import type { StreamClipRange } from './api/streams';

/**
 * Client-side mirror of the clip-edit limits in internal/streamclips/types.go
 * (defaultOverlayFontSize, min/maxOverlayFontSize, maxTextOverlaysPerClip,
 * maxClipFadeSeconds). Keep in lockstep with the Go constants.
 */
export const DEFAULT_OVERLAY_FONT_SIZE = 64;
export const MIN_OVERLAY_FONT_SIZE = 24;
export const MAX_OVERLAY_FONT_SIZE = 120;
export const MAX_TEXT_OVERLAYS = 4;
export const MAX_CLIP_FADE_SECONDS = 5;

/** Playback rates the render's chained atempo filters reproduce faithfully. */
export const CLIP_SPEEDS = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 2, 3] as const;

/** Immediate, localized range validation shared by editing and submission. */
export function streamRangeIssue(clip: StreamClipRange, durationSeconds: number, index: number): string | null {
  const label = clip.title?.trim() || `Clip ${index + 1}`;
  if (!Number.isFinite(clip.start_seconds) || clip.start_seconds < 0) {
    return `${label}: el inicio debe ser un número igual o mayor que 0.`;
  }
  if (!Number.isFinite(clip.end_seconds) || clip.end_seconds <= clip.start_seconds) {
    return `${label}: el fin debe ser posterior al inicio.`;
  }
  if (durationSeconds > 0 && clip.end_seconds > durationSeconds) {
    return `${label}: el fin supera la duración del vídeo (${durationSeconds.toFixed(2)} s).`;
  }
  return null;
}

export function streamRangesIssue(clips: StreamClipRange[], durationSeconds: number): string | null {
  if (clips.length === 0) return 'Añade al menos un rango de clip.';
  for (const [index, clip] of clips.entries()) {
    const issue = streamRangeIssue(clip, durationSeconds, index);
    if (issue !== null) return issue;
  }
  return null;
}

/**
 * First clip-edit problem the Go validator would reject the plan for, as a
 * Spanish message pointing at the offending clip; null when everything fits.
 * Mirrors streamclips.ClipEdit.validate for the states the editor can produce,
 * so the user gets a field-level message instead of a raw backend error.
 */
export function clipEditIssue(clips: StreamClipRange[]): string | null {
  for (const [index, clip] of clips.entries()) {
    const edit = clip.edit;
    if (!edit) continue;
    const label = clip.title?.trim() || `Clip ${index + 1}`;
    const fadeIn = edit.fade_in_seconds ?? 0;
    const fadeOut = edit.fade_out_seconds ?? 0;
    if (fadeIn < 0 || fadeIn > MAX_CLIP_FADE_SECONDS || fadeOut < 0 || fadeOut > MAX_CLIP_FADE_SECONDS) {
      return `${label}: cada fundido debe durar entre 0 y ${MAX_CLIP_FADE_SECONDS} segundos.`;
    }
    const speed = edit.speed ?? 1;
    const clipDuration = clip.end_seconds - clip.start_seconds;
    if (fadeIn + fadeOut > clipDuration / speed) {
      return `${label}: los fundidos no caben en la duración del clip a ${speed}x.`;
    }
    for (const overlay of edit.text_overlays ?? []) {
      if (overlay.text.trim() === '') {
        return `${label}: hay un texto en pantalla vacío.`;
      }
      if (
        overlay.font_size !== undefined &&
        (!Number.isInteger(overlay.font_size) ||
          overlay.font_size < MIN_OVERLAY_FONT_SIZE ||
          overlay.font_size > MAX_OVERLAY_FONT_SIZE)
      ) {
        return `${label}: el tamaño del texto debe ser un entero entre ${MIN_OVERLAY_FONT_SIZE} y ${MAX_OVERLAY_FONT_SIZE}.`;
      }
      const start = overlay.start_seconds;
      const end = overlay.end_seconds;
      if (start !== undefined && (start < 0 || start >= clipDuration)) {
        return `${label}: el inicio de un texto queda fuera del clip.`;
      }
      if (end !== undefined && (end <= 0 || end > clipDuration)) {
        return `${label}: el fin de un texto queda fuera del clip.`;
      }
      if (start !== undefined && end !== undefined && end <= start) {
        return `${label}: un texto termina antes de empezar.`;
      }
    }
  }
  return null;
}
