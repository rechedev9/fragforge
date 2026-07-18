import type { StreamCaptionWord, StreamClipRange, StreamEditPlan, StreamProbe } from './api/streams';

/** Go omits audio_codec for sources without audio, so absence means silent. */
export function streamHasAudio(probe?: StreamProbe): boolean {
  return (probe?.audio_codec ?? '') !== '';
}

/** Source audio is audible only when its explicit gain is not zero. */
export function clipHasAudibleSource(clip: StreamClipRange): boolean {
  return (clip.edit?.source_volume ?? 1) !== 0;
}

/** True when rendering must remain blocked pending an explicit human decision. */
export function captionsNeedReview(plan: StreamEditPlan, sourceHasAudio = true): boolean {
  if (!plan.captions?.enabled || !sourceHasAudio) return false;
  return plan.clips.some((clip) => clipHasAudibleSource(clip) && clip.caption_reviewed !== true);
}

/** Removes a review that no longer describes the clip's current audio range. */
export function invalidateCaptionReview(clip: StreamClipRange): StreamClipRange {
  const next = { ...clip };
  delete next.caption_words;
  delete next.caption_reviewed;
  return next;
}

/** True when the editor draft no longer matches the last approved plan words. */
export function captionDraftDiffersFromReview(clip: StreamClipRange, words: StreamCaptionWord[]): boolean {
  if (!clip.caption_reviewed) return false;
  const approved = clip.caption_words ?? [];
  return JSON.stringify(approved) !== JSON.stringify(words);
}

/** Fields bound into the backend's candidate fingerprint. */
export function captionInputsFingerprint(clips: StreamClipRange[]): string {
  return JSON.stringify(
    clips.map((clip) => [
      clip.id,
      clip.start_seconds,
      clip.end_seconds,
      clip.edit?.speed ?? 1,
      clip.edit?.source_volume ?? 1,
    ]),
  );
}

/** Local mirror of the word-cue validation needed for a useful review form. */
export function captionWordsIssue(words: StreamCaptionWord[], clipDuration: number): string | null {
  if (words.length === 0) return 'Añade al menos una palabra o marca el clip como sin voz.';
  let lastEnd = 0;
  for (const [index, cue] of words.entries()) {
    const label = `Palabra ${index + 1}`;
    if (cue.word.trim() === '') return `${label}: escribe el texto.`;
    if (Array.from(cue.word).length > 80) return `${label}: el texto supera 80 caracteres.`;
    if (/\r|\n/.test(cue.word)) return `${label}: el texto no puede contener saltos de línea.`;
    if ([cue.start_seconds, cue.end_seconds].some((value) => !Number.isFinite(value))) {
      return `${label}: los tiempos deben ser números.`;
    }
    if (cue.start_seconds < 0 || cue.end_seconds <= cue.start_seconds) {
      return `${label}: el final debe ser posterior al inicio.`;
    }
    if (cue.end_seconds - cue.start_seconds > 2.5) {
      return `${label}: una palabra no puede durar más de 2,5 segundos.`;
    }
    if (cue.end_seconds > clipDuration + 0.001) {
      return `${label}: el tiempo supera la duración del clip.`;
    }
    if (index > 0 && cue.start_seconds < lastEnd) {
      return `${label}: los tiempos se solapan o están desordenados.`;
    }
    lastEnd = cue.end_seconds;
  }
  return null;
}
