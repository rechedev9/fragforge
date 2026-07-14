import type { KillfeedKill, StreamClipRange, StreamEditPlan } from './api/streams';

/**
 * Keeps a clip's killfeed cues and their kills consistent: drops cues outside
 * the clip range, sorts by timestamp, dedupes (preferring the entry that has
 * kills), and keeps `killfeed_kills` index-aligned with `killfeed_seconds`.
 *
 * The Go plan accepts either an omitted `killfeed_kills` or one whose length
 * matches `killfeed_seconds`, so this emits `killfeed_kills` only when at least
 * one cue has kills; otherwise it omits it and every cue keeps the frozen-crop
 * behavior.
 */
export function normalizeClipKillfeed(clip: StreamClipRange): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  const kills = clip.killfeed_kills ?? [];
  const pairs = cues
    .map((cue, index) => ({ cue, kills: kills[index] ?? [] }))
    .filter(
      ({ cue }) =>
        Number.isFinite(cue) && cue >= clip.start_seconds && cue < clip.end_seconds,
    )
    .sort((left, right) => left.cue - right.cue);

  const deduped: { cue: number; kills: KillfeedKill[] }[] = [];
  for (const pair of pairs) {
    const previous = deduped[deduped.length - 1];
    if (previous && previous.cue === pair.cue) {
      if (previous.kills.length === 0 && pair.kills.length > 0) previous.kills = pair.kills;
      continue;
    }
    deduped.push({ cue: pair.cue, kills: pair.kills });
  }

  const next = { ...clip };
  if (deduped.length > 0) {
    next.killfeed_seconds = deduped.map((pair) => pair.cue);
  } else {
    delete next.killfeed_seconds;
  }
  if (deduped.some((pair) => pair.kills.length > 0)) {
    next.killfeed_kills = deduped.map((pair) => pair.kills);
  } else {
    delete next.killfeed_kills;
  }
  return next;
}

/**
 * Normalizes every clip's killfeed cues/kills, and when the plan has no
 * killfeed crop strips all cues and kills so a render never carries orphaned
 * killfeed data.
 */
export function normalizeKillfeedPlan(plan: StreamEditPlan): StreamEditPlan {
  const clips = plan.clips.map((clip) => {
    if (!plan.killfeed_crop) {
      const withoutKillfeed = { ...clip };
      delete withoutKillfeed.killfeed_seconds;
      delete withoutKillfeed.killfeed_kills;
      return withoutKillfeed;
    }
    return normalizeClipKillfeed(clip);
  });
  return { ...plan, clips };
}

/** Adds a killfeed cue with no kills yet, keeping cue/kill alignment. */
export function addClipCue(clip: StreamClipRange, cue: number): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  if (cues.includes(cue)) return normalizeClipKillfeed(clip);
  const kills = clip.killfeed_kills;
  return normalizeClipKillfeed({
    ...clip,
    killfeed_seconds: [...cues, cue],
    killfeed_kills: kills ? [...kills, []] : undefined,
  });
}

/** Removes a killfeed cue and its aligned kills entry by index. */
export function removeClipCue(clip: StreamClipRange, cue: number): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  const index = cues.indexOf(cue);
  if (index < 0) return normalizeClipKillfeed(clip);
  return normalizeClipKillfeed({
    ...clip,
    killfeed_seconds: cues.filter((_value, i) => i !== index),
    killfeed_kills: (clip.killfeed_kills ?? []).filter((_value, i) => i !== index),
  });
}

/** Replaces the kills for the cue at `cue` in `clip`, keeping alignment. */
export function setClipCueKills(
  clip: StreamClipRange,
  cue: number,
  kills: KillfeedKill[],
): StreamClipRange {
  const cues = clip.killfeed_seconds ?? [];
  const index = cues.indexOf(cue);
  if (index < 0) return clip;
  const nextKills = cues.map((_value, i) => (i === index ? kills : clip.killfeed_kills?.[i] ?? []));
  return normalizeClipKillfeed({ ...clip, killfeed_kills: nextKills });
}
